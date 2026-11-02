package service

import (
	"context"
	"errors"
	"net/url"
	"path"
	"strings"
	"sync"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
)

// EnsureMembershipEpayRecovery can adopt an old order only when its stored
// checkout proves the original merchant configuration. It never signs a new
// checkout as evidence for an old order and never creates a paid receipt.
func EnsureMembershipEpayRecovery(ctx context.Context, order *model.MembershipOrder) (*model.MembershipPaymentCheck, error) {
	if order == nil || order.ID <= 0 || order.Provider != model.PaymentProviderEpay || order.Currency != "CNY" {
		return nil, ErrMembershipPaymentEvidence
	}
	check, err := model.GetMembershipPaymentCheck(ctx, order.ID)
	if err == nil && order.ExceptionReason == model.MembershipLegacyCredentialReview {
		return model.EnsureMembershipPaymentCheck(ctx, order.ID, check.MerchantID, check.EncryptedSnapshot)
	}
	if err == nil || !errors.Is(err, gorm.ErrRecordNotFound) {
		return check, err
	}
	if order.CheckoutState != "ready" || order.CheckoutData == "" || len(order.CheckoutData) > 65536 {
		return nil, ErrMembershipPaymentEvidence
	}
	var checkout struct {
		PaymentURL string            `json:"payment_url"`
		SubmitURL  string            `json:"submit_url"`
		FormFields map[string]string `json:"form_fields"`
	}
	if common.UnmarshalStrict([]byte(order.CheckoutData), &checkout) != nil || checkout.PaymentURL != "" {
		return nil, ErrMembershipPaymentEvidence
	}
	params := checkout.FormFields
	for field := range params {
		switch field {
		case "pid", "type", "out_trade_no", "notify_url", "name", "money", "device", "sign_type", "return_url", "sign":
		default:
			return nil, ErrMembershipPaymentEvidence
		}
	}
	address, merchantID, key := operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey
	base, err := url.Parse(address)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || merchantID == "" || key == "" {
		return nil, ErrMembershipPaymentUnavailable
	}
	base.Path = path.Join(base.Path, epay.PurchaseUrl)
	if checkout.SubmitURL != base.String() || params["pid"] != merchantID || params["out_trade_no"] != order.TradeNo || params["type"] != order.PaymentMethod || params["sign_type"] != "MD5" || params["name"] == "" || (params["device"] != "pc" && params["device"] != "mobile") {
		return nil, ErrMembershipPaymentEvidence
	}
	for _, field := range []string{"notify_url", "return_url"} {
		target, err := url.Parse(params[field])
		if err != nil || target.Scheme != "https" || target.Host == "" || target.User != nil {
			return nil, ErrMembershipPaymentEvidence
		}
	}
	if strings.TrimSpace(params["money"]) != params["money"] || strings.ContainsAny(params["money"], "eE+-") {
		return nil, ErrMembershipPaymentEvidence
	}
	amount, err := model.ParseCNYMinor(params["money"])
	if err != nil || amount <= 0 || amount != order.AmountMinor {
		return nil, ErrMembershipPaymentEvidence
	}
	client, err := epay.NewClient(&epay.Config{PartnerID: merchantID, Key: key}, address)
	if err != nil {
		return nil, ErrMembershipPaymentUnavailable
	}
	verified, err := client.Verify(params)
	if err != nil || verified == nil || !verified.VerifyStatus {
		return nil, ErrMembershipPaymentEvidence
	}
	sealed, err := CreateMembershipEpaySnapshot(order, address, merchantID, key)
	if err != nil {
		return nil, err
	}
	return model.EnsureMembershipPaymentCheck(ctx, order.ID, merchantID, sealed)
}

// The cursor affects discovery efficiency only. Durable checks, receipts and
// fulfillment leases remain in the database. An invalid old checkout cannot
// starve later orders; a restart simply scans the bounded batches again.
var membershipLegacyDiscovery = struct {
	sync.Mutex
	db     *gorm.DB
	cursor int64
}{}

func discoverLegacyMembershipEpayPayments(ctx context.Context, limit int) (int, int, error) {
	membershipLegacyDiscovery.Lock()
	defer membershipLegacyDiscovery.Unlock()
	if membershipLegacyDiscovery.db != model.DB {
		membershipLegacyDiscovery.db, membershipLegacyDiscovery.cursor = model.DB, 0
	}
	if limit <= 0 || limit > 20 {
		limit = 20
	}
	checks := model.DB.Model(&model.MembershipPaymentCheck{}).Select("1").Where("order_id = kkai_membership_orders.id")
	var orders []model.MembershipOrder
	err := model.DB.WithContext(ctx).Where("id > ? AND provider = ? AND currency = ? AND status IN ? AND paid_at = 0 AND resolved_at = 0 AND checkout_state = ? AND checkout_data <> ?", membershipLegacyDiscovery.cursor, model.PaymentProviderEpay, "CNY", []string{model.MembershipOrderPending, model.MembershipOrderClosed}, "ready", "").
		Where("NOT EXISTS (?)", checks).Order("id").Limit(limit).Find(&orders).Error
	if err != nil {
		return 0, 0, err
	}
	imported, review := 0, 0
	for _, order := range orders {
		if err := ctx.Err(); err != nil {
			return imported, review, err
		}
		_, err := EnsureMembershipEpayRecovery(ctx, &order)
		if err == nil {
			imported++
		} else if errors.Is(err, ErrMembershipPaymentEvidence) || errors.Is(err, ErrMembershipPaymentUnavailable) {
			if saveErr := model.RecordMembershipLegacyReview(ctx, order.ID, model.MembershipLegacyCredentialReview); saveErr != nil && !errors.Is(saveErr, gorm.ErrRecordNotFound) && !errors.Is(saveErr, model.ErrMembershipConflict) {
				return imported, review, saveErr
			}
			review++
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			review++
		} else if errors.Is(err, ErrWalletSecretUnavailable) {
			// The process encryption key can be restored without changing this
			// order's proven checkout. Do not turn that outage into a review.
		} else {
			return imported, review, err
		}
		membershipLegacyDiscovery.cursor = order.ID
	}
	if len(orders) < limit {
		membershipLegacyDiscovery.cursor = 0
	}
	return imported, review, nil
}
