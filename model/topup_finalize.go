package model

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const (
	KKAIOutboxTopicTopUpCompleted = "kkai.billing.topup.completed"
	topUpRebateActiveFromIDEnv    = "TOPUP_REBATE_ACTIVE_FROM_ID"
)

var (
	ErrTopUpFinalizationInvalidInput = errors.New("invalid topup finalization input")
	ErrTopUpQuotaInvalid             = errors.New("invalid topup quota delta")
	ErrTopUpPaymentProviderInvalid   = errors.New("invalid topup payment provider")
	errTopUpRebateBoundaryInvalid    = errors.New("invalid topup rebate active boundary")
)

type TopUpUserPatch struct {
	StripeCustomer *string
	EmailIfEmpty   *string
}

type TopUpCompletion struct {
	QuotaDelta    int64
	PaymentMethod string
	UserPatch     TopUpUserPatch
}

type PrepareTopUpCompletion func(*TopUp, *User) (TopUpCompletion, error)

type FinalizeTopUpInput struct {
	TradeNo          string
	ExpectedProvider string
	Prepare          PrepareTopUpCompletion
	CompletedAt      int64
}

type FinalizeTopUpResult struct {
	TopUp            TopUp
	QuotaDelta       int64
	AlreadyCompleted bool
}

type TopUpCompletedEvent struct {
	SchemaVersion   int    `json:"schema_version"`
	EventKey        string `json:"event_key"`
	EventType       string `json:"event_type"`
	SourceOrderID   int64  `json:"source_order_id"`
	InviteeID       int64  `json:"invitee_id"`
	InviterID       *int64 `json:"inviter_id"`
	InviterGroup    string `json:"inviter_group"`
	CreditedQuota   int64  `json:"credited_quota"`
	CompletedAt     int64  `json:"completed_at"`
	PaymentProvider string `json:"payment_provider"`
}

func FinalizeTopUp(input FinalizeTopUpInput) (*FinalizeTopUpResult, error) {
	if strings.TrimSpace(input.TradeNo) == "" || input.Prepare == nil {
		return nil, ErrTopUpFinalizationInvalidInput
	}
	completedAt := input.CompletedAt
	if completedAt <= 0 {
		completedAt = common.GetTimestamp()
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		result, err := finalizeTopUpOnce(input, completedAt)
		if err == nil {
			invalidateTopUpUserCache(result)
			return result, nil
		}
		lastErr = err
		completed, lookupErr := completedTopUpReplay(input)
		if lookupErr == nil && completed != nil {
			return completed, nil
		}
		if !isSQLiteBusyError(err) {
			return nil, err
		}
		time.Sleep(time.Duration(attempt+1) * 5 * time.Millisecond)
	}
	return nil, lastErr
}

func finalizeTopUpOnce(input FinalizeTopUpInput, completedAt int64) (*FinalizeTopUpResult, error) {
	result := &FinalizeTopUpResult{}
	if err := DB.Transaction(func(tx *gorm.DB) error {
		topUp, err := lockTopUpByTradeNo(tx, input.TradeNo)
		if err != nil {
			return err
		}
		if input.ExpectedProvider != "" && topUp.PaymentProvider != input.ExpectedProvider {
			return ErrPaymentMethodMismatch
		}
		result.TopUp = *topUp
		if topUp.Status == common.TopUpStatusSuccess {
			result.AlreadyCompleted = true
			return nil
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}

		user := &User{}
		if err := lockForUpdate(tx).Where("id = ?", topUp.UserId).First(user).Error; err != nil {
			return err
		}
		completion, err := input.Prepare(topUp, user)
		if err != nil {
			return err
		}
		if completion.QuotaDelta <= 0 {
			return ErrTopUpQuotaInvalid
		}
		if completion.PaymentMethod != "" {
			topUp.PaymentMethod = completion.PaymentMethod
		}
		topUp.Status = common.TopUpStatusSuccess
		topUp.CompleteTime = completedAt
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}

		updates := map[string]any{"quota": gorm.Expr("quota + ?", completion.QuotaDelta)}
		if completion.UserPatch.StripeCustomer != nil {
			updates["stripe_customer"] = *completion.UserPatch.StripeCustomer
		}
		if completion.UserPatch.EmailIfEmpty != nil && user.Email == "" {
			updates["email"] = *completion.UserPatch.EmailIfEmpty
		}
		maximumQuotaBeforeCredit := int64(math.MaxInt64) - completion.QuotaDelta
		if user.Quota > maximumQuotaBeforeCredit {
			return ErrTopUpQuotaInvalid
		}
		updated := userQuotaCreditQuery(tx, user.Id, completion.QuotaDelta).Updates(updates)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrTopUpQuotaInvalid
		}

		activeFromID, err := topUpRebateActiveFromID()
		if err != nil {
			return err
		}
		if int64(topUp.Id) >= activeFromID {
			if err := enqueueTopUpCompletedEvent(tx, topUp, user, completion.QuotaDelta, completedAt); err != nil {
				return err
			}
		}

		result.TopUp = *topUp
		result.QuotaDelta = completion.QuotaDelta
		return nil
	}); err != nil {
		return nil, err
	}
	return result, nil
}

func topUpRebateActiveFromID() (int64, error) {
	raw := strings.TrimSpace(os.Getenv(topUpRebateActiveFromIDEnv))
	if raw == "" {
		return 1, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, errTopUpRebateBoundaryInvalid
	}
	return value, nil
}

func enqueueTopUpCompletedEvent(tx *gorm.DB, topUp *TopUp, invitee *User, creditedQuota int64, completedAt int64) error {
	payload, err := buildTopUpCompletedEvent(tx, topUp, invitee, creditedQuota, completedAt)
	if err != nil {
		return err
	}
	payloadJSON, err := common.Marshal(payload)
	if err != nil {
		return err
	}
	return tx.Create(&KKAIOutboxEvent{
		EventKey:    payload.EventKey,
		Topic:       KKAIOutboxTopicTopUpCompleted,
		AggregateID: fmt.Sprintf("%d", topUp.Id),
		Payload:     string(payloadJSON),
		Status:      KKAIOutboxStatusPending,
		AvailableAt: completedAt,
		CreatedAt:   completedAt,
	}).Error
}

func completedTopUpReplay(input FinalizeTopUpInput) (*FinalizeTopUpResult, error) {
	topUp, err := FindTopUpByTradeNo(input.TradeNo)
	if err != nil {
		return nil, err
	}
	if input.ExpectedProvider != "" && topUp.PaymentProvider != input.ExpectedProvider {
		return nil, ErrPaymentMethodMismatch
	}
	if topUp.Status != common.TopUpStatusSuccess {
		return nil, ErrTopUpStatusInvalid
	}
	return &FinalizeTopUpResult{TopUp: *topUp, AlreadyCompleted: true}, nil
}

func invalidateTopUpUserCache(result *FinalizeTopUpResult) {
	if result == nil || result.AlreadyCompleted {
		return
	}
	if err := InvalidateUserCache(result.TopUp.UserId); err != nil {
		common.SysLog("failed to invalidate user cache after topup finalization")
	}
}

func isSQLiteBusyError(err error) bool {
	if err == nil || !common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "sqlite_busy") || strings.Contains(message, "database is locked")
}

func lockTopUpByTradeNo(tx *gorm.DB, tradeNo string) (*TopUp, error) {
	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}
	topUp := &TopUp{}
	if err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTopUpNotFound
		}
		return nil, err
	}
	return topUp, nil
}

func buildTopUpCompletedEvent(tx *gorm.DB, topUp *TopUp, invitee *User, creditedQuota int64, completedAt int64) (*TopUpCompletedEvent, error) {
	inviterGroup := "default"
	var inviterID *int64
	if invitee.InviterId > 0 {
		inviter := &User{}
		if err := lockForUpdate(tx).Select("id", "group").Where("id = ?", invitee.InviterId).First(inviter).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, err
			}
			common.SysLog(fmt.Sprintf("topup inviter missing; completing without inviter invitee_id=%d inviter_id=%d topup_id=%d", invitee.Id, invitee.InviterId, topUp.Id))
		} else {
			value := int64(inviter.Id)
			inviterID = &value
			if strings.TrimSpace(inviter.Group) != "" {
				inviterGroup = strings.TrimSpace(inviter.Group)
			}
		}
	}
	eventKey := fmt.Sprintf("newapi:topup:%d", topUp.Id)
	return &TopUpCompletedEvent{
		SchemaVersion:   2,
		EventKey:        eventKey,
		EventType:       "topup.completed",
		SourceOrderID:   int64(topUp.Id),
		InviteeID:       int64(topUp.UserId),
		InviterID:       inviterID,
		InviterGroup:    inviterGroup,
		CreditedQuota:   creditedQuota,
		CompletedAt:     completedAt,
		PaymentProvider: topUp.PaymentProvider,
	}, nil
}

func quotaFromTopUpMoney(money float64) (int64, error) {
	if math.IsNaN(money) || math.IsInf(money, 0) || money <= 0 {
		return 0, ErrTopUpQuotaInvalid
	}
	return topUpQuotaFromDecimal(
		decimal.NewFromFloat(money).Mul(decimal.NewFromFloat(common.QuotaPerUnit)),
	)
}

func quotaFromTopUpAmount(amount int64) (int64, error) {
	if amount <= 0 {
		return 0, ErrTopUpQuotaInvalid
	}
	return topUpQuotaFromDecimal(
		decimal.NewFromInt(amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)),
	)
}

func quotaFromTopUpCredits(quota int64) (int64, error) {
	return topUpQuotaFromDecimal(decimal.NewFromInt(quota))
}

func topUpQuotaFromDecimal(quota decimal.Decimal) (int64, error) {
	rounded := quota.Round(0)
	if !rounded.IsPositive() || rounded.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return 0, ErrTopUpQuotaInvalid
	}
	return rounded.IntPart(), nil
}
