package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/model"
)

var (
	ErrMembershipPaymentUnavailable = errors.New("membership payment verification unavailable")
	ErrMembershipPaymentEvidence    = errors.New("membership payment evidence does not match order")
)

// MembershipEpaySnapshot is encrypted at order creation. It is never returned
// through a checkout, task payload, or payment response.
type MembershipEpaySnapshot struct {
	Version       int
	APIURL        string
	MerchantID    string
	APIKey        string
	PaymentMethod string
}

func membershipEpayScope(order *model.MembershipOrder, merchantID string) string {
	return fmt.Sprintf("membership-payment-v1/%d/%s/epay/%s", order.UserID, order.TradeNo, merchantID)
}

func CreateMembershipEpaySnapshot(order *model.MembershipOrder, address, merchantID, key string) (string, error) {
	if order == nil || order.UserID <= 0 || !strings.HasPrefix(order.TradeNo, "MEMBER-") || order.Provider != model.PaymentProviderEpay || order.Currency != "CNY" || order.PaymentMethod == "" || merchantID == "" || key == "" {
		return "", ErrMembershipPaymentEvidence
	}
	base, err := url.Parse(address)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return "", ErrMembershipPaymentUnavailable
	}
	return SealWalletPayment(MembershipEpaySnapshot{Version: 1, APIURL: address, MerchantID: merchantID, APIKey: key, PaymentMethod: order.PaymentMethod}, membershipEpayScope(order, merchantID))
}

func MembershipEpayGateway(ctx context.Context, order *model.MembershipOrder) (*MembershipEpaySnapshot, error) {
	if order == nil || order.Provider != model.PaymentProviderEpay || order.Currency != "CNY" {
		return nil, ErrMembershipPaymentEvidence
	}
	if err := CheckMembershipPaymentRuntimeSchema(ctx, model.DB); err != nil {
		return nil, err
	}
	check, err := EnsureMembershipEpayRecovery(ctx, order)
	if err != nil {
		return nil, err
	}
	var snapshot MembershipEpaySnapshot
	if err := OpenWalletPayment(check.EncryptedSnapshot, membershipEpayScope(order, check.MerchantID), &snapshot); err != nil {
		return nil, ErrMembershipPaymentUnavailable
	}
	if snapshot.Version != 1 || snapshot.MerchantID != check.MerchantID || snapshot.APIKey == "" || snapshot.PaymentMethod != order.PaymentMethod || check.UserID != order.UserID || check.TradeNo != order.TradeNo {
		return nil, ErrMembershipPaymentEvidence
	}
	base, err := url.Parse(snapshot.APIURL)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, ErrMembershipPaymentEvidence
	}
	return &snapshot, nil
}

// Ownership and administrator authorization belong to the request handler.
// This entry only schedules verification and accepts no client payment facts.
func RequestMembershipPaymentCheck(ctx context.Context, order *model.MembershipOrder, source string) error {
	if source != "self" && source != "admin" {
		return ErrMembershipPaymentEvidence
	}
	if !MembershipPaymentReconcileEnabled() {
		return ErrMembershipPaymentUnavailable
	}
	if _, err := MembershipEpayGateway(ctx, order); err != nil {
		return err
	}
	if err := model.WakeMembershipPaymentCheck(ctx, order.ID, source); err != nil {
		return err
	}
	notifySystemTaskRunner()
	return nil
}

// HandleMembershipEpayCallback treats even a correctly signed success callback
// only as a durable request to query the original merchant. A signed checkout
// or a replay cannot create a payment receipt or grant membership.
func HandleMembershipEpayCallback(ctx context.Context, params map[string]string) error {
	tradeNo := params["out_trade_no"]
	if !strings.HasPrefix(tradeNo, "MEMBER-") {
		return ErrMembershipPaymentEvidence
	}
	order, err := model.GetMembershipOrder(tradeNo, 0)
	if err != nil {
		return err
	}
	snapshot, err := MembershipEpayGateway(ctx, order)
	if err != nil {
		return err
	}
	client, err := epay.NewClient(&epay.Config{PartnerID: snapshot.MerchantID, Key: snapshot.APIKey}, snapshot.APIURL)
	if err != nil {
		return ErrMembershipPaymentUnavailable
	}
	verified, err := client.Verify(params)
	if err != nil || verified == nil || !verified.VerifyStatus || params["pid"] != snapshot.MerchantID || verified.ServiceTradeNo != order.TradeNo {
		return ErrMembershipPaymentEvidence
	}
	if verified.TradeStatus != epay.StatusTradeSuccess {
		return nil
	}
	// The query verifies the paid amount and transaction independently; callback
	// payment-method aliases cannot block that recovery or become paid facts.
	if err := model.WakeMembershipPaymentCheck(ctx, order.ID, "callback"); err != nil {
		if errors.Is(err, model.ErrMembershipPaymentReview) || errors.Is(err, model.ErrMembershipConflict) {
			check, readErr := model.GetMembershipPaymentCheck(ctx, order.ID)
			if readErr != nil {
				return readErr
			}
			if check.Status == model.MembershipPaymentVerified || check.Status == model.MembershipPaymentComplete || check.Status == model.MembershipPaymentReview {
				return nil
			}
		}
		return err
	}
	notifySystemTaskRunner()
	return nil
}
