package service

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// PrepareMembershipReservation quotes both sources from the same undiscounted
// maximum. Source selection and available balances are checked in one DB tx.
func PrepareMembershipReservation(info *relaycommon.RelayInfo, maximumBeforeGroup float64) (model.MembershipReservationInput, error) {
	if info == nil || maximumBeforeGroup < 0 || math.IsNaN(maximumBeforeGroup) || math.IsInf(maximumBeforeGroup, 0) {
		return model.MembershipReservationInput{}, model.ErrMembershipInvalidInput
	}
	base := ratio_setting.GetGroupRatio(info.UsingGroup)
	walletRatio := info.PriceData.GroupRatioInfo.GroupRatio
	quote, err := model.GetMembershipQuote(info.UserId, info.UsingGroup, base, walletRatio)
	if err != nil {
		return model.MembershipReservationInput{}, err
	}
	snapshot := types.MembershipPricing{
		Version: 1, GroupKey: info.UsingGroup, ModelName: info.GetBillingModelName(),
		TierID: quote.TierID, ReleaseID: quote.ReleaseID, AccountID: quote.AccountID, AccountRevision: quote.Revision,
		BaseRatio: base, WalletRatio: walletRatio, Discount: quote.Discount,
		Price: info.PriceData, OtherRatios: info.PriceData.OtherRatios(), QuotaPerUnit: common.QuotaPerUnit,
		ToolPrices:            operation_setting.SnapshotToolPrices(info.GetBillingModelName()),
		GeminiAudioInputPrice: operation_setting.GetGeminiInputAudioPricePerMillionTokens(info.GetBillingModelName()),
		MaximumBeforeGroup:    maximumBeforeGroup,
	}
	if previous := info.MembershipPricing; previous != nil {
		snapshot.MaxOutputTokens = previous.MaxOutputTokens
		snapshot.MaxInputTokens = previous.MaxInputTokens
		snapshot.OutputCopies = previous.OutputCopies
		snapshot.ToolCallLimits = previous.ToolCallLimits
		snapshot.RequiresOutputBound = previous.RequiresOutputBound
		snapshot.ToolPrices = previous.ToolPrices
		snapshot.QuotaPerUnit = previous.QuotaPerUnit
		snapshot.GeminiAudioInputPrice = previous.GeminiAudioInputPrice
		snapshot.ViolationFeeBeforeGroup = previous.ViolationFeeBeforeGroup
	}
	if info.TieredBillingSnapshot != nil {
		copy := *info.TieredBillingSnapshot
		snapshot.Tiered = &copy
	}
	if info.ImagePricingSnapshot != nil {
		copy := *info.ImagePricingSnapshot
		snapshot.Image = &copy
	}
	input := model.MembershipReservationInput{
		RequestID: info.RequestId, UserID: info.UserId, TokenID: info.TokenId, TokenKey: info.TokenKey,
		SkipToken: info.IsPlayground, GroupKey: info.UsingGroup,
		ExpectedAccountID: quote.AccountID, ExpectedRevision: quote.Revision, ExpectedReleaseID: quote.ReleaseID,
	}
	for _, member := range []bool{false, true} {
		ratio := quote.WalletRatio
		if member {
			ratio = quote.MembershipRatio
		}
		quota, err := common.QuotaFromFloatStrict(math.Ceil(maximumBeforeGroup * ratio))
		if err != nil {
			return input, err
		}
		snapshot.Price.GroupRatioInfo.GroupRatio = ratio
		snapshot.Price.QuotaToPreConsume = quota
		snapshot.Price.Quota = quota
		snapshot.Price.FreeModel = quota == 0
		if snapshot.Tiered != nil {
			snapshot.Tiered.GroupRatio = ratio
			snapshot.Tiered.EstimatedQuotaAfterGroup = quota
		}
		if snapshot.Image != nil {
			snapshot.Image.GroupRatio = ratio
		}
		encoded, err := common.Marshal(snapshot)
		if err != nil {
			return input, err
		}
		if member {
			input.SubscriptionQuota = int64(quota)
			input.SubscriptionSnapshot = string(encoded)
		} else {
			input.WalletQuota = int64(quota)
			input.WalletSnapshot = string(encoded)
		}
	}
	return input, nil
}

// ApplyMembershipReservation restores the committed quote. It never reloads
// mutable pricing after the payer has authorized a request.
func ApplyMembershipReservation(info *relaycommon.RelayInfo, record *model.MembershipBillingRecord) error {
	var snapshot types.MembershipPricing
	if record == nil || common.UnmarshalJsonStr(record.Snapshot, &snapshot) != nil || snapshot.Version != 1 || snapshot.GroupKey != record.GroupKey {
		return model.ErrMembershipInvalidInput
	}
	snapshot.Price.ReplaceOtherRatios(snapshot.OtherRatios)
	info.MembershipPricing = &snapshot
	info.PriceData = snapshot.Price
	info.TieredBillingSnapshot = snapshot.Tiered
	info.ImagePricingSnapshot = snapshot.Image
	info.BillingSource = record.Source
	info.MembershipBillingRequestID = record.RequestID
	info.FinalPreConsumedQuota = int(record.ReservedQuota)
	info.UsingGroup = snapshot.GroupKey
	return nil
}

func preConsumeMembershipBilling(info *relaycommon.RelayInfo) *relaytypes.NewAPIError {
	if info.MembershipPricing == nil {
		return membershipBillingError(fmt.Errorf("membership maximum budget was not prepared"))
	}
	input, err := PrepareMembershipReservation(info, info.MembershipPricing.MaximumBeforeGroup)
	if err != nil {
		return membershipBillingError(err)
	}
	record, err := model.ReserveMembershipBilling(input)
	if err != nil {
		return membershipBillingError(err)
	}
	if err := ApplyMembershipReservation(info, record); err != nil {
		// A malformed persisted quote needs investigation; never refund an existing
		// request automatically when its upstream outcome may already be known.
		return membershipBillingError(err)
	}
	info.Billing = &membershipBillingSession{info: info, requestID: record.RequestID, reserved: int(record.ReservedQuota), state: record.State}
	return nil
}

type membershipBillingSession struct {
	mu        sync.Mutex
	info      *relaycommon.RelayInfo
	requestID string
	reserved  int
	state     string
}

func (s *membershipBillingSession) Settle(actualQuota int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if actualQuota < 0 {
		return model.ErrMembershipInvalidInput
	}
	record, err := model.SettleMembershipRelayBilling(s.requestID, int64(actualQuota), s.info.ChannelId, s.info.MembershipHasBillableUsage || s.info.MembershipViolationFee > 0)
	if err != nil {
		s.state = "settlement_pending"
		_ = model.MarkMembershipBillingUncertain(s.requestID, s.info.ChannelId, "settlement_failed")
		return err
	}
	s.state = record.State
	s.info.MembershipAssessedQuota = int(record.ActualQuota)
	s.info.MembershipChargedQuota = int(record.ChargedQuota)
	return nil
}
func (s *membershipBillingSession) Refund(c *gin.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != "reserved" {
		return
	}
	record, err := model.ReleaseMembershipBilling(s.requestID)
	if err != nil {
		logger.LogError(c, "membership release failed: "+err.Error())
		return
	}
	s.state = record.State
}
func (s *membershipBillingSession) NeedsRefund() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state == "reserved"
}
func (s *membershipBillingSession) GetPreConsumedQuota() int { return s.reserved }
func (s *membershipBillingSession) Reserve(targetQuota int) error {
	if targetQuota < 0 || targetQuota > s.reserved {
		return fmt.Errorf("request exceeds its frozen membership authorization")
	}
	return nil
}

// MembershipCharge preserves the assessed charge for the durable ledger while
// returning the capped charge used by usage statistics and the public log.
func MembershipCharge(info *relaycommon.RelayInfo, actual int) int {
	if info == nil || info.MembershipBillingRequestID == "" {
		return actual
	}
	assessed, clamp := common.QuotaFromFloatChecked(float64(max(actual, 0)) + float64(info.MembershipViolationFee))
	noteQuotaClamp(info, clamp)
	info.MembershipAssessedQuota = assessed
	info.MembershipChargedQuota = min(assessed, info.FinalPreConsumedQuota)
	return info.MembershipChargedQuota
}

func membershipBillingError(err error) *relaytypes.NewAPIError {
	status, code := http.StatusConflict, relaytypes.ErrorCodeUpdateDataError
	if errors.Is(err, model.ErrMembershipInsufficientQuota) {
		status, code = http.StatusForbidden, relaytypes.ErrorCodeInsufficientUserQuota
	} else if errors.Is(err, model.ErrTaskBillingInsufficientToken) {
		status, code = http.StatusForbidden, relaytypes.ErrorCodePreConsumeTokenQuotaFailed
		err = fmt.Errorf("API key cannot authorize this request; check its quota or use a new key: %w", err)
	} else if errors.Is(err, model.ErrMembershipInvalidInput) {
		status, code = http.StatusBadRequest, relaytypes.ErrorCodeInvalidRequest
	}
	return relaytypes.NewErrorWithStatusCode(err, code, status, relaytypes.ErrOptionWithSkipRetry())
}

// CaptureFailedRequestUsage keeps billable partial output on the original
// authorization. An indeterminate response retains its reservation for review;
// it must not be retried or refunded as a confirmed zero-cost failure.
func CaptureFailedRequestUsage(c *gin.Context, info *relaycommon.RelayInfo, usage any, apiErr *relaytypes.NewAPIError) {
	if !info.HasDurableBilling() || apiErr == nil {
		return
	}
	relaytypes.ErrOptionWithSkipRetry()(apiErr)
	if membershipStreamIncomplete(info) {
		info.BillingOutcomeUnknown = true
		return
	}
	switch value := usage.(type) {
	case *dto.Usage:
		if value != nil && (info.BillingHasReportedUsage || value.TotalTokens > 0 || value.PromptTokens > 0 || value.CompletionTokens > 0) {
			info.BillingFailureUsage = value
			return
		}
	case *dto.RealtimeUsage:
		if value != nil && value.TotalTokens > 0 {
			info.BillingFailureUsage = value
			return
		}
	}
	// This callback runs only after an accepted upstream response was parsed.
	// Missing/malformed usage is not evidence that the provider billed nothing.
	info.BillingOutcomeUnknown = true
}

func membershipStreamIncomplete(info *relaycommon.RelayInfo) bool {
	if !info.IsStream || info.StreamStatus == nil {
		return false
	}
	outcome := info.StreamStatus.OutcomeSnapshot()
	return outcome.Response == relaycommon.ResponseOutcomeUnknown && outcome.EndReason != relaycommon.StreamEndReasonDone
}

// FinalizeFailedRequestUsage runs after the terminal policy decision, so a
// configured violation fee and returned usage settle against one authorization.
func FinalizeFailedRequestUsage(c *gin.Context, info *relaycommon.RelayInfo, apiErr *relaytypes.NewAPIError) bool {
	if info.BillingFailureUsage == nil {
		return false
	}
	if info.TenantID == 0 && info.MembershipPricing != nil && shouldChargeViolationFee(c, apiErr) {
		info.MembershipViolationFee = common.QuotaRound(info.MembershipPricing.ViolationFeeBeforeGroup * info.PriceData.GroupRatioInfo.GroupRatio)
	}
	switch value := info.BillingFailureUsage.(type) {
	case *dto.Usage:
		if value.PromptTokensDetails.AudioTokens > 0 || value.CompletionTokenDetails.AudioTokens > 0 {
			PostAudioConsumeQuota(c, info, value, "")
		} else {
			PostTextConsumeQuota(c, info, value, nil)
		}
	case *dto.RealtimeUsage:
		PostWssConsumeQuota(c, info, info.GetBillingModelName(), value, "")
	}
	info.BillingFailureUsage = nil
	return true
}
