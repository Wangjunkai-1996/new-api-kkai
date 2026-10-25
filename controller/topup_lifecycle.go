package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type walletQuoteClaims struct {
	UserID        int                     `json:"user_id"`
	TenantID      int64                   `json:"tenant_id"`
	Input         dto.WalletQuoteRequest  `json:"input"`
	ExpiresAt     int64                   `json:"expires_at"`
	ExpectedMinor int64                   `json:"expected_minor"`
	Currency      string                  `json:"currency"`
	Exponent      int                     `json:"exponent"`
	CreditedQuota int64                   `json:"credited_quota"`
	StoredAmount  int64                   `json:"stored_amount"`
	LegacyMoney   float64                 `json:"legacy_money"`
	CreditDisplay dto.WalletCreditDisplay `json:"credit_display"`
	PricingMode   string                  `json:"pricing_mode"`
	ConfigDigest  string                  `json:"config_digest"`
	AccountRef    string                  `json:"account_ref"`
	Environment   string                  `json:"environment"`
}

var walletKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)
var walletAmountPattern = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)

func walletPositiveFinite(values ...float64) bool {
	for _, value := range values {
		if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return true
}

func walletHash(value any) string {
	data, _ := common.Marshal(value)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
func walletScope(c *gin.Context) model.WalletTopUpScope {
	tenant, _ := middleware.GetTenantContext(c)
	return model.WalletTopUpScope{TenantID: tenant.TenantID, UserID: c.GetInt("id")}
}
func walletSchemaReady(c *gin.Context) bool {
	if !model.WalletPaymentSchemaAvailable() {
		walletError(c, 503, "WALLET_STATE_UNAVAILABLE", nil)
		return false
	}
	if err := service.CheckWalletPaymentRuntimeSchema(c.Request.Context(), model.DB); err != nil {
		walletError(c, 503, "WALLET_STATE_UNAVAILABLE", nil)
		return false
	}
	return true
}
func walletBind(c *gin.Context, target any) bool {
	if !walletSchemaReady(c) {
		return false
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 16385))
	if err != nil || len(body) > 16384 || common.Unmarshal(body, target) != nil {
		walletError(c, http.StatusBadRequest, "VALIDATION_FAILED", nil)
		return false
	}
	return true
}
func walletHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
}
func walletSuccess(c *gin.Context, status int, data any) {
	walletHeaders(c)
	c.JSON(status, gin.H{"success": true, "message": "", "data": data})
}
func walletError(c *gin.Context, status int, code string, wallet *dto.WalletState) {
	walletHeaders(c)
	out := gin.H{"success": false, "code": code, "message": code}
	if wallet != nil {
		out["data"] = gin.H{"wallet": wallet}
	}
	c.JSON(status, out)
}
func walletModelError(c *gin.Context, err error) {
	code := "WALLET_STATE_UNAVAILABLE"
	status := http.StatusServiceUnavailable
	switch {
	case errors.Is(err, model.ErrTopUpNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		code = "ORDER_NOT_FOUND"
		status = 404
	case errors.Is(err, model.ErrTopUpQuotaLimitExceeded):
		code = "QUOTA_CAPACITY_EXCEEDED"
		status = 409
	case errors.Is(err, model.ErrWalletFlowActive):
		code = "CURRENT_FLOW_ACTIVE"
		status = 409
	case errors.Is(err, model.ErrWalletFlowChanged):
		code = "FLOW_CHANGED"
		status = 409
	case errors.Is(err, model.ErrWalletOrderChanged):
		code = "ORDER_CHANGED"
		status = 409
	case errors.Is(err, model.ErrWalletAttemptEnded):
		code = "PAYMENT_ATTEMPT_ENDED"
		status = 409
	case errors.Is(err, model.ErrTopUpStatusInvalid):
		code = "RESULT_NOT_FINAL"
		status = 409
	case errors.Is(err, model.ErrWalletIdempotencyConflict):
		code = "IDEMPOTENCY_CONFLICT"
		status = 409
	default:
		// Model lifecycle errors carry stable public codes; database errors never do.
		for _, candidate := range []string{"CURRENT_FLOW_ACTIVE", "FLOW_CHANGED", "ORDER_CHANGED", "IDEMPOTENCY_CONFLICT", "RESULT_NOT_FINAL", "PAYMENT_NOT_RESUMABLE", "CANCEL_UNSUPPORTED", "PAYMENT_CHECK_RATE_LIMITED"} {
			if err != nil && err.Error() == candidate {
				code = candidate
				status = 409
				if code == "PAYMENT_CHECK_RATE_LIMITED" {
					status = 429
					c.Header("Retry-After", "5")
				}
				break
			}
		}
	}
	var wallet *dto.WalletState
	if status == 409 || status == 429 {
		if state, e := model.GetWalletTopUpState(walletScope(c)); e == nil {
			projected := walletStateDTO(state)
			wallet = &projected
		}
	}
	walletError(c, status, code, wallet)
}
func walletString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
func walletTime(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	return &value
}
func WalletTopUpOrderDTO(order *model.TopUp, current bool) dto.WalletOrder {
	status, group, phase := walletOrderStatus(order)
	_ = phase
	deadline := model.WalletTopUpDeadline(order)
	out := dto.WalletOrder{TradeNo: order.TradeNo, Version: strconv.FormatInt(order.Version, 10), DisplayStatus: status, StatusGroup: group, AllowedActions: []string{}, PricingMode: order.PricingMode, PaymentProvider: order.PaymentProvider, PaymentMethod: order.PaymentMethod, CreatedAt: order.CreateTime, DeadlineAt: walletTime(deadline), DeadlineSource: "local_policy", LocalEndReason: walletString(order.LocalEndReason), LocalEndedAt: walletTime(order.LocalEndedAt), CloseReason: walletString(order.CloseReason), ClosedAt: walletTime(order.ClosedAt), CompletedAt: walletTime(order.CompleteTime), LastCheckedAt: walletTime(order.LastCheckedAt), ReasonCode: walletString(order.ReasonCode), PricingNoticeCode: walletString(order.PricingNoticeCode)}
	if deadline <= 0 {
		out.DeadlineSource = "unknown"
	} else if order.ProviderExpiresAt > 0 && deadline == order.ProviderExpiresAt || order.DeadlineSource == "provider" && deadline == order.PayDeadlineAt {
		out.DeadlineSource = "provider"
	}
	localEnded := model.WalletTopUpLocalEnded(order, time.Now().Unix())
	if localEnded && order.LocalEndedAt == 0 {
		out.LocalEndReason = walletString("timeout")
		out.LocalEndedAt = walletTime(deadline)
	}
	if out.PricingMode == "" {
		out.PricingMode = "unknown"
	}
	if order.Currency != "" && order.PaidMinor > 0 {
		out.ExpectedPayment = &dto.WalletMoney{AmountMinor: strconv.FormatInt(order.PaidMinor, 10), Currency: order.Currency, Exponent: order.CurrencyExponent}
		if order.WalletLifecycleVersion == 0 {
			out.ExpectedPayment.Exponent = 2
		}
	}
	if order.PayableMinor != nil {
		out.PayablePayment = &dto.WalletMoney{AmountMinor: strconv.FormatInt(*order.PayableMinor, 10), Currency: order.Currency, Exponent: order.CurrencyExponent}
	}
	if order.CreditedQuota > 0 {
		q := strconv.FormatInt(order.CreditedQuota, 10)
		out.CreditedQuota = &q
		out.CreditDisplay = &dto.WalletCreditDisplay{Amount: order.CreditDisplayAmount, Unit: order.CreditDisplayUnit}
	}
	if order.WalletLifecycleVersion == 0 {
		amount := strconv.FormatInt(order.Amount, 10)
		money := strconv.FormatFloat(order.Money, 'f', -1, 64)
		out.LegacyDisplay = &dto.WalletLegacyDisplay{Amount: &amount, Money: &money}
	}
	if status == "review_required" || status == "credit_review" {
		out.Review = &dto.WalletReview{ReasonCode: order.ReasonCode, OwnerLabel: order.ReviewOwner, LastCheckedAt: walletTime(order.LastCheckedAt)}
		out.AllowedActions = append(out.AllowedActions, "contact_support")
	}
	canAct := !localEnded && !model.WalletTopUpReliableTerminal(order) && (deadline > 0 || status == "credit_review")
	if status != "success" && canAct {
		out.AllowedActions = append(out.AllowedActions, "reconcile")
	}
	if canAct && status != "success" && status != "credit_review" && order.ProviderPaymentState != "paid" {
		out.AllowedActions = append(out.AllowedActions, "end")
	}
	if status == "pending" && order.WalletLifecycleVersion > 0 {
		var snapshot service.WalletGatewaySnapshot
		credentialReady := order.CheckoutPayload != "" && service.OpenWalletPayment(order.CheckoutPayload, service.WalletPaymentScope(order), &snapshot) == nil
		if credentialReady && snapshot.CanResume && service.ValidWalletCheckout(snapshot.Checkout) && (snapshot.TokenExpiresAt == 0 || snapshot.TokenExpiresAt > time.Now().Unix()) {
			out.AllowedActions = append(out.AllowedActions, "resume")
		}
		if credentialReady && order.PaymentProvider == model.PaymentProviderStripe && order.ProviderSessionID != "" {
			out.AllowedActions = append(out.AllowedActions, "cancel")
		}
	}
	if current && (model.WalletTopUpReliableTerminal(order) || localEnded) {
		out.AllowedActions = append(out.AllowedActions, "acknowledge")
	}
	return out
}

// Only committed accounting receipts supply the user-facing actual amount.
func walletAttachReceivedPayment(order *dto.WalletOrder, id int) error {
	if order.DisplayStatus != "success" {
		return nil
	}
	receipts, err := model.GetWalletCreditedReceipts([]int{id})
	if err != nil {
		return err
	}
	if receipt, ok := receipts[id]; ok {
		order.ReceivedPayment = &dto.WalletMoney{AmountMinor: strconv.FormatInt(receipt.AmountMinor, 10), Currency: receipt.Currency, Exponent: receipt.CurrencyExponent}
	}
	return nil
}
func walletTimeline(id int) ([]dto.WalletTimelineEvent, error) {
	events, err := model.GetWalletPaymentEvents(id)
	if err != nil {
		return nil, err
	}
	timeline := make([]dto.WalletTimelineEvent, 0, len(events))
	for _, event := range events {
		summary := event.ReasonCode
		if summary == "" {
			summary = event.Status
		}
		timeline = append(timeline, dto.WalletTimelineEvent{EventType: event.Kind, OccurredAt: event.CreatedAt, SummaryCode: summary})
	}
	return timeline, nil
}
func walletOrderStatus(order *model.TopUp) (string, string, string) {
	if order.Status == common.TopUpStatusSuccess {
		return "success", "completed", "result"
	}
	if order.Status == "credit_review" || order.ProviderPaymentState == "paid" {
		return "credit_review", "review", "review"
	}
	if order.LocalEndedAt > 0 {
		if order.LocalEndReason == "user_ended" {
			return "ended", "closed", "result"
		}
		return "timed_out", "closed", "result"
	}
	if model.WalletTopUpReliableTerminal(order) {
		return order.Status, "closed", "result"
	}
	if model.WalletTopUpLocalEnded(order, time.Now().Unix()) {
		return "timed_out", "closed", "result"
	}
	if model.WalletTopUpDeadline(order) <= 0 {
		return "unavailable", "all", "review"
	}
	if order.Status == "credit_review" || order.Status == "review_required" {
		return order.Status, "review", "review"
	}
	if order.WalletLifecycleVersion == 0 {
		return "unavailable", "all", "review"
	}
	if order.CreateState == "creating" || order.CreateState == "unknown" {
		return "creating", "processing", "creating"
	}
	if order.CloseRequestedAt > 0 {
		if order.CloseReason == "timeout" {
			return "expiry_processing", "processing", "processing"
		}
		return "cancel_processing", "processing", "processing"
	}
	if order.ProviderPaymentState == "processing" {
		return "payment_processing", "processing", "processing"
	}
	if order.Status == common.TopUpStatusPending && order.CreateState == "ready" {
		return "pending", "pending", "order"
	}
	return "unavailable", "all", "review"
}
func walletStateDTO(state *model.WalletTopUpState) dto.WalletState {
	out := dto.WalletState{Flow: dto.WalletFlow{Revision: "0", Phase: "idle"}, CanCreate: true, OtherUnfinishedCount: state.OtherUnfinishedCount, ServerTime: time.Now().Unix()}
	if state.Flow != nil {
		out.Flow.Revision = strconv.FormatInt(state.Flow.Revision, 10)
	}
	if state.Order != nil && state.Flow != nil {
		out.Flow.ID = &state.Flow.FlowID
		order := WalletTopUpOrderDTO(state.Order, true)
		out.Flow.Order = &order
		_, _, out.Flow.Phase = walletOrderStatus(state.Order)
		out.CanCreate = false
		out.CreateBlockReasonCode = walletString("CURRENT_FLOW_ACTIVE")
	}
	if out.CanCreate && state.HasRecoverableCandidate {
		out.CanCreate = false
		out.CreateBlockReasonCode = walletString("RECOVERABLE_ORDER_EXISTS")
	}
	if out.CanCreate && !operation_setting.IsPaymentComplianceConfirmed() {
		out.CanCreate = false
		out.CreateBlockReasonCode = walletString("PAYMENT_DISABLED")
	}
	if out.CanCreate && (!service.WalletPaymentSecretsReady() || !(isEpayTopUpEnabled() || isStripeTopUpEnabled() || isCreemWalletTopUpEnabled() || isWaffoTopUpEnabled() || isWaffoPancakeTopUpEnabled())) {
		out.CanCreate = false
		out.CreateBlockReasonCode = walletString("CHANNEL_UNAVAILABLE")
	}
	return out
}
func walletStateWithReceipt(state *model.WalletTopUpState) (dto.WalletState, error) {
	out := walletStateDTO(state)
	if out.Flow.Order != nil && state.Order != nil {
		if err := walletAttachReceivedPayment(out.Flow.Order, state.Order.Id); err != nil {
			return out, err
		}
	}
	return out, nil
}
func GetWalletTopUpCurrent(c *gin.Context) {
	if !walletSchemaReady(c) {
		return
	}
	state, err := model.GetWalletTopUpState(walletScope(c))
	if err != nil {
		walletModelError(c, err)
		return
	}
	wallet, err := walletStateWithReceipt(state)
	if err != nil {
		walletModelError(c, err)
		return
	}
	walletSuccess(c, 200, wallet)
}
func RestoreWalletTopUpCurrent(c *gin.Context) {
	var req struct {
		TradeNo string `json:"trade_no"`
	}
	if !walletBind(c, &req) {
		return
	}
	if len(req.TradeNo) > 255 {
		walletError(c, 400, "VALIDATION_FAILED", nil)
		return
	}
	state, err := model.RestoreWalletTopUp(walletScope(c), req.TradeNo)
	if err != nil {
		walletModelError(c, err)
		return
	}
	wallet, err := walletStateWithReceipt(state)
	if err != nil {
		walletModelError(c, err)
		return
	}
	walletSuccess(c, 200, wallet)
}
func walletCurrencyExponent(currency string) (int, error) {
	switch currency {
	case "JPY", "KRW", "VND", "CLP", "RWF", "XAF", "XOF", "XPF", "BIF", "DJF", "GNF", "KMF", "PYG", "VUV":
		return 0, nil
	case "BHD", "JOD", "KWD", "OMR", "TND":
		return 3, nil
	case "USD", "CNY", "EUR", "GBP", "HKD", "AUD", "CAD", "SGD", "NZD", "CHF", "INR", "MYR", "THB", "PHP", "BRL", "MXN", "ZAR", "AED", "SAR", "TRY", "PLN", "CZK", "HUF", "NOK", "SEK", "DKK", "ILS", "RON", "BGN", "RUB", "IDR":
		return 2, nil
	}
	return 0, errors.New("unsupported currency")
}
func walletQuote(c *gin.Context, input dto.WalletQuoteRequest) (*walletQuoteClaims, *service.WalletGatewaySnapshot, error) {
	scope := walletScope(c)
	if !service.WalletPaymentSecretsReady() || !operation_setting.IsPaymentComplianceConfirmed() {
		return nil, nil, errors.New("CREATE_DISABLED")
	}
	if !walletPositiveFinite(common.QuotaPerUnit) {
		return nil, nil, errors.New("CREATE_DISABLED")
	}
	user, err := model.GetUserById(scope.UserID, false)
	if err != nil || user == nil || user.TenantID != scope.TenantID {
		return nil, nil, errors.New("CREATE_DISABLED")
	}
	q := &walletQuoteClaims{UserID: scope.UserID, TenantID: scope.TenantID, Input: input, ExpiresAt: time.Now().Add(5 * time.Minute).Unix(), PricingMode: "fixed"}
	s := &service.WalletGatewaySnapshot{Provider: input.PaymentProvider, Environment: "prod", Email: user.Email, Username: user.Username, CustomerID: user.StripeCustomer, ReturnURL: paymentReturnPath("/wallet"), CanResume: true}
	var amount int64
	if input.ProductID == "" {
		if !walletAmountPattern.MatchString(input.Amount) {
			return nil, nil, errors.New("VALIDATION_FAILED")
		}
		amount, err = strconv.ParseInt(input.Amount, 10, 64)
		if err != nil {
			return nil, nil, errors.New("VALIDATION_FAILED")
		}
	} else if input.Amount != "" {
		return nil, nil, errors.New("VALIDATION_FAILED")
	}
	if scope.TenantID > 0 && input.PaymentProvider != model.PaymentProviderEpay {
		return nil, nil, errors.New("CREATE_DISABLED")
	}
	var pay decimal.Decimal
	switch input.PaymentProvider {
	case model.PaymentProviderEpay:
		if !isEpayTopUpEnabled() || input.ProductID != "" || amount < getMinTopup() || !operation_setting.ContainsPayMethod(input.PaymentMethod) || (scope.TenantID > 0 && input.PaymentMethod != "alipay" && input.PaymentMethod != "wxpay") {
			return nil, nil, errors.New("VALIDATION_FAILED")
		}
		q.CreditedQuota, err = validateTopUpQuota(amount)
		pay = getPayMoneyDecimal(amount, user.Group, scope.TenantID > 0).Round(2)
		s.Currency = "CNY"
		s.MerchantID = operation_setting.EpayId
		s.APIKey = operation_setting.EpayKey
		s.APIURL = operation_setting.PayAddress
		s.NotifyURL = service.GetCallbackAddress() + "/api/user/epay/notify"
		q.AccountRef = "epay:" + walletHash([]string{s.APIURL, s.MerchantID})
		if scope.TenantID > 0 {
			tenant, _ := middleware.GetTenantContext(c)
			s.ReturnURL = "https://" + tenant.Hostname + "/wallet"
		}
		q.StoredAmount = amount
		if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
			q.StoredAmount = decimal.NewFromInt(amount).Div(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart()
		}
	case model.PaymentProviderStripe:
		if !isStripeTopUpEnabled() || input.PaymentMethod != model.PaymentMethodStripe || input.ProductID != "" || amount < getStripeMinTopup() || amount > 10000 {
			return nil, nil, errors.New("VALIDATION_FAILED")
		}
		q.CreditedQuota, err = validateCreditedQuota(getStripeCreditedQuota(amount, user.Group))
		s.APIKey = setting.StripeApiSecret
		s.WebhookSecret = setting.StripeWebhookSecret
		s.PriceID = setting.StripePriceId
		s.Quantity = amount
		s.AllowPromotionCodes = setting.StripePromotionCodesEnabled && !model.MembershipTopupDiscountsDisabled()
		price, e := service.WalletStripePrice(c.Request.Context(), s.APIKey, s.PriceID)
		if e != nil || price == nil || !price.Active || price.UnitAmount <= 0 || price.BillingScheme != "per_unit" || price.TransformQuantity != nil || price.Recurring != nil {
			return nil, nil, errors.New("PAYMENT_PROVIDER_UNAVAILABLE")
		}
		s.Currency = strings.ToUpper(string(price.Currency))
		s.Exponent, err = walletCurrencyExponent(s.Currency)
		if err != nil {
			return nil, nil, errors.New("CREATE_DISABLED")
		}
		total := decimal.NewFromInt(price.UnitAmount).Mul(decimal.NewFromInt(amount))
		if total.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
			return nil, nil, errors.New("VALIDATION_FAILED")
		}
		pay = total.Shift(-int32(s.Exponent))
		q.StoredAmount = amount
		q.LegacyMoney = GetChargedAmount(float64(amount), *user)
		q.AccountRef, err = service.WalletStripeAccountID(c.Request.Context(), s.APIKey)
		if err != nil {
			return nil, nil, errors.New("PAYMENT_PROVIDER_UNAVAILABLE")
		}
		if !price.Livemode {
			s.Environment = "test"
		}
		if s.AllowPromotionCodes {
			q.PricingMode = "channel_adjusted"
		}
	case model.PaymentProviderCreem:
		if !isCreemWalletTopUpEnabled() {
			return nil, nil, errors.New("CREATE_DISABLED")
		}
		if input.PaymentMethod != model.PaymentMethodCreem || input.ProductID == "" {
			return nil, nil, errors.New("VALIDATION_FAILED")
		}
		var products []CreemProduct
		if common.UnmarshalJsonStr(setting.CreemProducts, &products) != nil {
			return nil, nil, errors.New("CREATE_DISABLED")
		}
		var selected *CreemProduct
		for _, p := range products {
			if p.ProductId == input.ProductID {
				p, err = creemProductWithoutBonus(p)
				selected = &p
				break
			}
		}
		if selected == nil {
			return nil, nil, errors.New("VALIDATION_FAILED")
		}
		if err != nil {
			return nil, nil, err
		}
		q.CreditedQuota = selected.Quota
		q.StoredAmount = selected.Quota
		if !walletPositiveFinite(selected.Price) {
			return nil, nil, errors.New("VALIDATION_FAILED")
		}
		pay = decimal.NewFromFloat(selected.Price)
		s.Currency = strings.ToUpper(selected.Currency)
		s.APIKey = setting.CreemApiKey
		s.WebhookSecret = setting.CreemWebhookSecret
		s.ProductID = selected.ProductId
		s.APIURL = "https://api.creem.io"
		if setting.CreemTestMode {
			s.Environment = "test"
			s.APIURL = "https://test-api.creem.io"
		}
		q.AccountRef = strings.TrimSpace(os.Getenv("WALLET_CREEM_ACCOUNT_REF"))
		if q.AccountRef == "" {
			return nil, nil, errors.New("CREATE_DISABLED")
		}
		q.PricingMode = "channel_adjusted"
	case model.PaymentProviderWaffo:
		if !isWaffoTopUpEnabled() || input.PaymentMethod != model.PaymentMethodWaffo || input.ProductID != "" || amount < int64(setting.WaffoMinTopUp) {
			return nil, nil, errors.New("VALIDATION_FAILED")
		}
		valid := input.WaffoPayMethodType == "" && input.WaffoPayMethodName == ""
		for _, m := range setting.GetWaffoPayMethods() {
			if m.PayMethodType == input.WaffoPayMethodType && m.PayMethodName == input.WaffoPayMethodName {
				valid = true
			}
		}
		if !valid {
			return nil, nil, errors.New("VALIDATION_FAILED")
		}
		q.CreditedQuota, err = validateTopUpQuota(amount)
		q.StoredAmount = normalizeWaffoPancakeTopUpAmount(amount)
		waffoMoney := getWaffoPayMoney(float64(amount), user.Group)
		if !walletPositiveFinite(waffoMoney) {
			return nil, nil, errors.New("VALIDATION_FAILED")
		}
		pay = decimal.NewFromFloat(waffoMoney)
		s.Currency = getWaffoCurrency()
		s.MerchantID = setting.WaffoMerchantId
		s.APIKey = setting.WaffoApiKey
		s.PrivateKey = setting.WaffoPrivateKey
		s.PublicKey = setting.WaffoPublicCert
		if setting.WaffoSandbox {
			s.Environment = "test"
			s.APIKey = setting.WaffoSandboxApiKey
			s.PrivateKey = setting.WaffoSandboxPrivateKey
			s.PublicKey = setting.WaffoSandboxPublicCert
		}
		s.PayMethodType = input.WaffoPayMethodType
		s.PayMethodName = input.WaffoPayMethodName
		s.NotifyURL = service.GetCallbackAddress() + "/api/waffo/webhook"
		if setting.WaffoNotifyUrl != "" {
			s.NotifyURL = setting.WaffoNotifyUrl
		}
		if setting.WaffoReturnUrl != "" {
			s.ReturnURL = setting.WaffoReturnUrl
		}
		q.AccountRef = "waffo:" + s.MerchantID
	case model.PaymentProviderWaffoPancake:
		if !isWaffoPancakeTopUpEnabled() || input.PaymentMethod != model.PaymentMethodWaffoPancake || input.ProductID != "" || amount < int64(setting.WaffoPancakeMinTopUp) {
			return nil, nil, errors.New("VALIDATION_FAILED")
		}
		q.CreditedQuota, err = validateTopUpQuota(amount)
		q.StoredAmount = normalizeWaffoPancakeTopUpAmount(amount)
		pancakeMoney := getWaffoPancakePayMoney(amount, user.Group)
		if !walletPositiveFinite(pancakeMoney) {
			return nil, nil, errors.New("VALIDATION_FAILED")
		}
		pay = decimal.NewFromFloat(pancakeMoney)
		s.Currency = "USD"
		s.MerchantID = setting.WaffoPancakeMerchantID
		s.StoreID = setting.WaffoPancakeStoreID
		if s.StoreID == "" {
			return nil, nil, errors.New("CREATE_DISABLED")
		}
		s.PrivateKey = setting.WaffoPancakePrivateKey
		s.ProductID = setting.WaffoPancakeProductID
		s.BuyerIdentity = getWaffoPancakeBuyerIdentity(user)
		s.Email = getWaffoPancakeBuyerEmail(user)
		q.AccountRef = "pancake:" + s.MerchantID
		q.PricingMode = "channel_adjusted"
	default:
		return nil, nil, errors.New("VALIDATION_FAILED")
	}
	if err != nil || q.CreditedQuota <= 0 {
		return nil, nil, errors.New("VALIDATION_FAILED")
	}
	if err = model.ValidateTopUpQuotaCapacity(scope.UserID, q.CreditedQuota); err != nil {
		return nil, nil, err
	}
	s.Exponent, err = walletCurrencyExponent(s.Currency)
	if err != nil {
		return nil, nil, errors.New("CREATE_DISABLED")
	}
	minor := pay.Round(int32(s.Exponent)).Shift(int32(s.Exponent))
	if !minor.IsPositive() || minor.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return nil, nil, errors.New("VALIDATION_FAILED")
	}
	q.ExpectedMinor = minor.IntPart()
	q.Currency = s.Currency
	q.Exponent = s.Exponent
	q.Environment = s.Environment
	s.AmountText = pay.StringFixed(int32(s.Exponent))
	if q.LegacyMoney == 0 {
		q.LegacyMoney = pay.InexactFloat64()
	}
	q.CreditDisplay = dto.WalletCreditDisplay{Amount: decimal.NewFromInt(q.CreditedQuota).Div(decimal.NewFromFloat(common.QuotaPerUnit)).String(), Unit: "USD"}
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		q.CreditDisplay.Amount = strconv.FormatInt(q.CreditedQuota, 10)
		q.CreditDisplay.Unit = "TOKENS"
	}
	q.ConfigDigest = walletHash(s)
	return q, s, nil
}
func walletQuoteResponse(q *walletQuoteClaims, token string) dto.WalletQuote {
	money := &dto.WalletMoney{AmountMinor: strconv.FormatInt(q.ExpectedMinor, 10), Currency: q.Currency, Exponent: q.Exponent}
	out := dto.WalletQuote{QuoteToken: token, ExpiresAt: q.ExpiresAt, ExpectedPayment: money, PricingMode: q.PricingMode, CreditedQuota: strconv.FormatInt(q.CreditedQuota, 10), CreditDisplay: q.CreditDisplay, PaymentProvider: q.Input.PaymentProvider, PaymentMethod: q.Input.PaymentMethod, ServerTime: time.Now().Unix()}
	if q.PricingMode == "fixed" {
		out.PayablePayment = money
	} else {
		out.PricingNoticeCode = walletString("CHANNEL_ADJUSTMENTS_APPLY")
	}
	return out
}
func QuoteWalletTopUp(c *gin.Context) {
	var req dto.WalletQuoteRequest
	if !walletBind(c, &req) {
		return
	}
	q, _, err := walletQuote(c, req)
	if err != nil {
		walletQuoteError(c, err)
		return
	}
	token, err := service.SignWalletQuote(q)
	if err != nil {
		walletModelError(c, err)
		return
	}
	walletSuccess(c, 200, walletQuoteResponse(q, token))
}
func walletQuoteError(c *gin.Context, err error) {
	switch err.Error() {
	case "VALIDATION_FAILED":
		walletError(c, 400, "VALIDATION_FAILED", nil)
	case "CREATE_DISABLED":
		walletError(c, 409, "CREATE_DISABLED", nil)
	case "PAYMENT_PROVIDER_UNAVAILABLE":
		walletError(c, 503, "PAYMENT_PROVIDER_UNAVAILABLE", nil)
	default:
		walletModelError(c, err)
	}
}

func walletRequestHash(q walletQuoteClaims) string { q.ExpiresAt = 0; return walletHash(q) }
func walletCreateResult(c *gin.Context, order *model.TopUp, status int) {
	state, err := model.GetWalletTopUpState(walletScope(c))
	if err != nil {
		walletModelError(c, err)
		return
	}
	current := state.Order != nil && state.Order.Id == order.Id
	out := WalletTopUpOrderDTO(order, current)
	if err := walletAttachReceivedPayment(&out, order.Id); err != nil {
		walletModelError(c, err)
		return
	}
	wallet, err := walletStateWithReceipt(state)
	if err != nil {
		walletModelError(c, err)
		return
	}
	walletSuccess(c, status, gin.H{"wallet": wallet, "order": out})
}
func CreateWalletTopUpOrder(c *gin.Context) {
	var req struct {
		QuoteToken           string `json:"quote_token"`
		ExpectedFlowRevision string `json:"expected_flow_revision"`
	}
	if !walletBind(c, &req) {
		return
	}
	key := c.GetHeader("Idempotency-Key")
	if !walletKeyPattern.MatchString(key) {
		walletError(c, 400, "VALIDATION_FAILED", nil)
		return
	}
	var quote walletQuoteClaims
	if service.VerifyWalletQuote(req.QuoteToken, &quote) != nil || quote.UserID != walletScope(c).UserID || quote.TenantID != walletScope(c).TenantID {
		walletError(c, 400, "VALIDATION_FAILED", nil)
		return
	}
	keyHash := walletHash(key)
	previous, err := model.FindWalletTopUpByRequestKey(walletScope(c), keyHash)
	if err == nil {
		if previous.RequestHash != walletRequestHash(quote) {
			walletError(c, 409, "IDEMPOTENCY_CONFLICT", nil)
			return
		}
		walletCreateResult(c, previous, 200)
		return
	}
	if !errors.Is(err, model.ErrTopUpNotFound) {
		walletModelError(c, err)
		return
	}
	if quote.ExpiresAt <= time.Now().Unix() {
		walletError(c, 409, "QUOTE_EXPIRED", nil)
		return
	}
	revision, err := strconv.ParseInt(req.ExpectedFlowRevision, 10, 64)
	if err != nil || revision < 0 {
		walletError(c, 400, "VALIDATION_FAILED", nil)
		return
	}
	fresh, gateway, err := walletQuote(c, quote.Input)
	if err != nil {
		walletQuoteError(c, err)
		return
	}
	if walletRequestHash(*fresh) != walletRequestHash(quote) {
		walletError(c, 409, "QUOTE_STALE", nil)
		return
	}
	walletCreate(c, &quote, gateway, keyHash, revision)
}
func walletCreate(c *gin.Context, quote *walletQuoteClaims, gateway *service.WalletGatewaySnapshot, keyHash string, revision int64) {
	scope := walletScope(c)
	trade := "WT" + common.GetRandomString(30)
	version, err := service.WalletPaymentCredentialVersion()
	if err != nil {
		walletQuoteError(c, errors.New("CREATE_DISABLED"))
		return
	}
	pricing, _ := common.Marshal(quote)
	order := &model.TopUp{TenantID: scope.TenantID, UserId: scope.UserID, TradeNo: trade, PaymentProvider: quote.Input.PaymentProvider, PaymentMethod: quote.Input.PaymentMethod, Amount: quote.StoredAmount, Money: quote.LegacyMoney, PaidMinor: quote.ExpectedMinor, Currency: quote.Currency, CurrencyExponent: quote.Exponent, CreditedQuota: quote.CreditedQuota, CreditDisplayAmount: quote.CreditDisplay.Amount, CreditDisplayUnit: quote.CreditDisplay.Unit, PricingMode: quote.PricingMode, PricingSnapshot: string(pricing), ProviderAccountRef: quote.AccountRef, ProviderEnvironment: quote.Environment, ProviderCredentialVersion: version, DeadlineSource: "unknown", ProviderPaymentState: "unknown"}
	if quote.PricingMode == "fixed" {
		value := quote.ExpectedMinor
		order.PayableMinor = &value
	} else {
		order.PricingNoticeCode = "CHANNEL_ADJUSTMENTS_APPLY"
	}
	order.CheckoutPayload, err = service.SealWalletPayment(gateway, service.WalletPaymentScope(order))
	if err != nil {
		walletModelError(c, err)
		return
	}
	order, _, replay, err := model.CreateWalletTopUp(scope, keyHash, walletRequestHash(*quote), revision, order)
	if err != nil {
		walletModelError(c, err)
		return
	}
	if replay {
		walletCreateResult(c, order, 200)
		return
	}
	remote, err := service.CreateWalletGatewaySession(c.Request.Context(), order, *gateway)
	if errors.Is(err, service.ErrWalletCreateNotStarted) {
		failed, e := model.MarkWalletTopUpCreateNotStarted(order.TradeNo, order.CreateClaimToken, "CREATE_NOT_STARTED")
		if e != nil {
			walletModelError(c, e)
			return
		}
		walletCreateResult(c, failed, 201)
		return
	}
	if err != nil || remote == nil || !service.ValidWalletCheckout(remote.Checkout) {
		unknown, e := model.MarkWalletTopUpCreateUnknown(order.TradeNo, order.CreateClaimToken, "CREATE_RESULT_UNKNOWN")
		if e != nil {
			walletModelError(c, e)
			return
		}
		_, _, _ = service.EnqueueSystemTask(service.WalletTopUpReconcileTaskType, nil)
		walletCreateResult(c, unknown, 202)
		return
	}
	gateway.Checkout = remote.Checkout
	gateway.TokenExpiresAt = remote.TokenExpiresAt
	cipher, e := service.SealWalletPayment(gateway, service.WalletPaymentScope(order))
	if e != nil {
		walletModelError(c, e)
		return
	}
	saved, e := model.SaveWalletTopUpSession(order.TradeNo, order.CreateClaimToken, remote.SessionID, remote.OrderID, cipher, remote.ExpiresAt)
	if e != nil {
		walletModelError(c, e)
		return
	}
	_, _, _ = service.EnqueueSystemTask(service.WalletTopUpReconcileTaskType, nil)
	walletCreateResult(c, saved, 201)
}
func GetWalletTopUpOrder(c *gin.Context) {
	if !walletSchemaReady(c) {
		return
	}
	order, err := model.FindWalletTopUpForUser(walletScope(c), c.Param("trade_no"))
	if err != nil {
		walletModelError(c, err)
		return
	}
	state, err := model.GetWalletTopUpState(walletScope(c))
	if err != nil {
		walletModelError(c, err)
		return
	}
	current := state.Order != nil && state.Order.Id == order.Id
	out := WalletTopUpOrderDTO(order, current)
	if err := walletAttachReceivedPayment(&out, order.Id); err != nil {
		walletModelError(c, err)
		return
	}
	timeline, err := walletTimeline(order.Id)
	if err != nil {
		walletModelError(c, err)
		return
	}
	wallet, err := walletStateWithReceipt(state)
	if err != nil {
		walletModelError(c, err)
		return
	}
	walletSuccess(c, 200, gin.H{"order": struct {
		dto.WalletOrder
		Timeline []dto.WalletTimelineEvent `json:"timeline"`
	}{out, timeline}, "wallet": wallet})
}
func ResumeWalletTopUpOrder(c *gin.Context) {
	var req struct {
		FlowID       string `json:"flow_id"`
		FlowRevision string `json:"flow_revision"`
	}
	if !walletBind(c, &req) {
		return
	}
	state, err := model.GetWalletTopUpState(walletScope(c))
	if err != nil {
		walletModelError(c, err)
		return
	}
	wallet, err := walletStateWithReceipt(state)
	if err != nil {
		walletModelError(c, err)
		return
	}
	if state.Flow == nil || state.Order == nil || state.Order.TradeNo != c.Param("trade_no") || state.Flow.FlowID != req.FlowID || strconv.FormatInt(state.Flow.Revision, 10) != req.FlowRevision {
		walletError(c, 409, "FLOW_CHANGED", &wallet)
		return
	}
	projected := WalletTopUpOrderDTO(state.Order, true)
	allowed := false
	for _, action := range projected.AllowedActions {
		if action == "resume" {
			allowed = true
		}
	}
	var gateway service.WalletGatewaySnapshot
	if !allowed || service.OpenWalletPayment(state.Order.CheckoutPayload, service.WalletPaymentScope(state.Order), &gateway) != nil || !gateway.CanResume || !service.ValidWalletCheckout(gateway.Checkout) || gateway.TokenExpiresAt > 0 && gateway.TokenExpiresAt <= time.Now().Unix() {
		walletError(c, 409, "PAYMENT_NOT_RESUMABLE", &wallet)
		return
	}
	walletSuccess(c, 200, gin.H{"wallet": wallet, "checkout": gateway.Checkout})
}
func CancelWalletTopUpOrder(c *gin.Context) {
	var req struct {
		Version string `json:"expected_order_version"`
	}
	if !walletBind(c, &req) {
		return
	}
	version, err := strconv.ParseInt(req.Version, 10, 64)
	if err != nil || version < 0 {
		walletError(c, 400, "VALIDATION_FAILED", nil)
		return
	}
	order, err := model.FindWalletTopUpForUser(walletScope(c), c.Param("trade_no"))
	if err != nil {
		walletModelError(c, err)
		return
	}
	if model.WalletTopUpLocalEnded(order, time.Now().Unix()) || model.WalletTopUpReliableTerminal(order) || model.WalletTopUpDeadline(order) <= 0 {
		walletModelError(c, model.ErrWalletAttemptEnded)
		return
	}
	if order.Status == "credit_review" || order.ProviderPaymentState == "paid" {
		walletModelError(c, model.ErrTopUpStatusInvalid)
		return
	}
	if order.PaymentProvider != model.PaymentProviderStripe || order.ProviderSessionID == "" {
		walletError(c, 409, "CANCEL_UNSUPPORTED", nil)
		return
	}
	order, err = model.RequestWalletTopUpCancel(walletScope(c), order.TradeNo, version, "user_cancel")
	if err != nil {
		walletModelError(c, err)
		return
	}
	_, _, err = service.EnqueueSystemTask(service.WalletTopUpReconcileTaskType, nil)
	if err != nil {
		walletModelError(c, err)
		return
	}
	walletCreateResult(c, order, 202)
}
func EndWalletTopUpOrder(c *gin.Context) {
	var req struct {
		Version string `json:"expected_order_version"`
	}
	if !walletBind(c, &req) {
		return
	}
	version, err := strconv.ParseInt(req.Version, 10, 64)
	if err != nil || version < 0 {
		walletError(c, 400, "VALIDATION_FAILED", nil)
		return
	}
	order, err := model.FindWalletTopUpForUser(walletScope(c), c.Param("trade_no"))
	if err != nil {
		walletModelError(c, err)
		return
	}
	if model.WalletTopUpLocalEnded(order, time.Now().Unix()) || model.WalletTopUpReliableTerminal(order) || model.WalletTopUpDeadline(order) <= 0 {
		walletModelError(c, model.ErrWalletAttemptEnded)
		return
	}
	order, err = model.EndWalletTopUpAttempt(walletScope(c), order.TradeNo, version, "user_ended")
	if err != nil {
		walletModelError(c, err)
		return
	}
	walletCreateResult(c, order, 200)
}
func ReconcileWalletTopUpOrder(c *gin.Context) {
	var req struct{}
	if !walletBind(c, &req) {
		return
	}
	order, err := model.FindWalletTopUpForUser(walletScope(c), c.Param("trade_no"))
	if err != nil {
		walletModelError(c, err)
		return
	}
	if !walletManualReconciliationAllowed(order) {
		walletModelError(c, model.ErrWalletAttemptEnded)
		return
	}
	order, err = model.ScheduleWalletTopUpCheck(walletScope(c), order.TradeNo)
	if err != nil {
		walletModelError(c, err)
		return
	}
	_, _, err = service.EnqueueSystemTask(service.WalletTopUpReconcileTaskType, nil)
	if err != nil {
		walletModelError(c, err)
		return
	}
	walletCreateResult(c, order, 202)
}
func AcknowledgeWalletTopUpCurrent(c *gin.Context) {
	var req struct {
		FlowID       string `json:"flow_id"`
		FlowRevision string `json:"flow_revision"`
		TradeNo      string `json:"trade_no"`
	}
	if !walletBind(c, &req) {
		return
	}
	revision, err := strconv.ParseInt(req.FlowRevision, 10, 64)
	if err != nil || revision < 0 || len(req.FlowID) > 64 || len(req.TradeNo) > 255 {
		walletError(c, 400, "VALIDATION_FAILED", nil)
		return
	}
	state, err := model.AcknowledgeWalletTopUp(walletScope(c), req.FlowID, revision, req.TradeNo)
	if errors.Is(err, model.ErrTopUpStatusInvalid) {
		walletError(c, 409, "RESULT_NOT_FINAL", nil)
		return
	}
	if err != nil {
		walletModelError(c, err)
		return
	}
	wallet, err := walletStateWithReceipt(state)
	if err != nil {
		walletModelError(c, err)
		return
	}
	walletSuccess(c, 200, wallet)
}
func GetAdminWalletTopUpOrder(c *gin.Context) {
	if !walletSchemaReady(c) {
		return
	}
	var target model.TopUp
	if err := model.WalletTopUpOnlyQuery(model.DB).Where("trade_no = ?", c.Param("trade_no")).First(&target).Error; err != nil {
		walletModelError(c, err)
		return
	}
	order := &target
	state, err := model.GetWalletTopUpState(walletScope(c))
	if err != nil {
		walletModelError(c, err)
		return
	}
	out := WalletTopUpOrderDTO(order, false)
	if err := walletAttachReceivedPayment(&out, order.Id); err != nil {
		walletModelError(c, err)
		return
	}
	timeline, err := walletTimeline(order.Id)
	if err != nil {
		walletModelError(c, err)
		return
	}
	receipts, err := model.GetWalletPaymentReceipts(order.Id)
	if err != nil {
		walletModelError(c, err)
		return
	}
	safeReceipts := make([]gin.H, 0, len(receipts))
	for _, r := range receipts {
		safeReceipts = append(safeReceipts, gin.H{"id": strconv.FormatInt(r.ID, 10), "payment": dto.WalletMoney{AmountMinor: strconv.FormatInt(r.AmountMinor, 10), Currency: r.Currency, Exponent: r.CurrencyExponent}, "paid_at": walletTime(r.PaidAt), "received_at": r.ReceivedAt, "verification_status": r.VerificationStatus, "processing_status": r.ProcessingStatus, "exception_reason": walletString(r.ExceptionReason), "review_owner": walletString(r.ReviewOwner), "evidence_source": r.EvidenceSource})
	}
	wallet, err := walletStateWithReceipt(state)
	if err != nil {
		walletModelError(c, err)
		return
	}
	walletSuccess(c, 200, gin.H{"order": struct {
		dto.WalletOrder
		UserID      int                       `json:"user_id"`
		ReviewOwner string                    `json:"review_owner"`
		Timeline    []dto.WalletTimelineEvent `json:"timeline"`
		Receipts    []gin.H                   `json:"receipts"`
	}{out, order.UserId, order.ReviewOwner, timeline, safeReceipts}, "wallet": wallet})
}
func AdminReconcileWalletTopUpOrder(c *gin.Context) {
	var req struct {
		Reason string `json:"reason"`
	}
	if !walletBind(c, &req) {
		return
	}
	if len(strings.TrimSpace(req.Reason)) < 2 || len(req.Reason) > 500 {
		walletError(c, 400, "VALIDATION_FAILED", nil)
		return
	}
	var target model.TopUp
	if err := model.WalletTopUpOnlyQuery(model.DB).Where("trade_no = ?", c.Param("trade_no")).First(&target).Error; err != nil {
		walletModelError(c, err)
		return
	}
	order := &target
	if !walletManualReconciliationAllowed(order) {
		walletModelError(c, model.ErrWalletAttemptEnded)
		return
	}
	_, err := model.ScheduleWalletTopUpCheck(model.WalletTopUpScope{TenantID: order.TenantID, UserID: order.UserId}, order.TradeNo)
	if err != nil {
		walletModelError(c, err)
		return
	}
	_, _, err = service.EnqueueSystemTask(service.WalletTopUpReconcileTaskType, nil)
	if err != nil {
		walletModelError(c, err)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("核实充值 trade_no=%s reason=%s", order.TradeNo, req.Reason))
	GetAdminWalletTopUpOrder(c)
}

func walletManualReconciliationAllowed(order *model.TopUp) bool {
	return !model.WalletTopUpLocalEnded(order, time.Now().Unix()) && !model.WalletTopUpReliableTerminal(order) && (model.WalletTopUpDeadline(order) > 0 || order.Status == "credit_review" || order.ProviderPaymentState == "paid")
}

// All five historical purchase routes share this guard and creation service.
// An old client cannot bypass current-flow exclusivity with a different URL.
func walletLegacyCreate(c *gin.Context, input dto.WalletQuoteRequest) {
	if !walletSchemaReady(c) {
		return
	}
	key := c.GetHeader("Idempotency-Key")
	if key == "" {
		walletError(c, 409, "CLIENT_UPGRADE_REQUIRED", nil)
		return
	}
	if !walletKeyPattern.MatchString(key) {
		walletError(c, 400, "VALIDATION_FAILED", nil)
		return
	}
	keyHash := walletHash(key)
	previous, err := model.FindWalletTopUpByRequestKey(walletScope(c), keyHash)
	if err == nil {
		var quote walletQuoteClaims
		if common.UnmarshalJsonStr(previous.PricingSnapshot, &quote) != nil || walletHash(quote.Input) != walletHash(input) {
			walletError(c, 409, "IDEMPOTENCY_CONFLICT", nil)
			return
		}
		walletCreateResult(c, previous, 200)
		return
	}
	if !errors.Is(err, model.ErrTopUpNotFound) {
		walletModelError(c, err)
		return
	}
	quote, gateway, err := walletQuote(c, input)
	if err != nil {
		walletQuoteError(c, err)
		return
	}
	state, err := model.GetWalletTopUpState(walletScope(c))
	if err != nil {
		walletModelError(c, err)
		return
	}
	revision := int64(0)
	if state.Flow != nil {
		revision = state.Flow.Revision
	}
	walletCreate(c, quote, gateway, keyHash, revision)
}
