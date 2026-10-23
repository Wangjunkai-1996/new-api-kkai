package service

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/shopspring/decimal"
	waffo "github.com/waffo-com/waffo-go"
	"github.com/waffo-com/waffo-go/config"
	"gorm.io/gorm"
)

func WalletGatewayForOrder(tradeNo, provider string) (*model.TopUp, *WalletGatewaySnapshot, error) {
	order, err := model.FindTopUpByTradeNo(tradeNo)
	if err != nil {
		return nil, nil, err
	}
	if order.PaymentProvider != provider {
		return order, nil, model.ErrPaymentMethodMismatch
	}
	if order.WalletLifecycleVersion != 1 {
		return order, nil, nil
	}
	if err := CheckWalletPaymentRuntimeSchema(context.Background(), model.DB); err != nil {
		return order, nil, err
	}
	var snapshot WalletGatewaySnapshot
	if err := OpenWalletPayment(order.CheckoutPayload, WalletPaymentScope(order), &snapshot); err != nil {
		return order, nil, err
	}
	if snapshot.Provider != provider || snapshot.Environment != order.ProviderEnvironment {
		return order, nil, model.ErrWalletPaymentEvidence
	}
	return order, &snapshot, nil
}
func HandleWalletEpayCallback(ctx context.Context, params map[string]string) (bool, error) {
	if !strings.HasPrefix(params["out_trade_no"], "WT") {
		return false, nil
	}
	order, snapshot, err := WalletGatewayForOrder(params["out_trade_no"], model.PaymentProviderEpay)
	if err != nil {
		return order != nil && order.WalletLifecycleVersion == 1, err
	}
	if snapshot == nil {
		return false, nil
	}
	client, err := epay.NewClient(&epay.Config{PartnerID: snapshot.MerchantID, Key: snapshot.APIKey}, snapshot.APIURL)
	if err != nil {
		return true, err
	}
	verified, err := client.Verify(params)
	if err != nil || verified == nil || !verified.VerifyStatus || params["pid"] != snapshot.MerchantID || verified.ServiceTradeNo != order.TradeNo || verified.Type != order.PaymentMethod {
		return true, model.ErrWalletPaymentEvidence
	}
	if verified.TradeStatus != epay.StatusTradeSuccess {
		return true, nil
	}
	minor, err := model.ParseCNYMinor(params["money"])
	if err != nil || minor <= 0 || params["trade_no"] == "" {
		return true, model.ErrWalletPaymentEvidence
	}
	// Epay requires a plain success response within five seconds. Persist the
	// signed paid fact before ACK; the independent reconciler queries the
	// original merchant for paidAt rather than blocking this callback on HTTP.
	_, err = FinalizeWalletPaymentReceipt(order, params["trade_no"], minor, "CNY", 2, 0, "verified_callback", "epay-notify:"+params["trade_no"], minor == order.PaidMinor, model.TopUpUserPatch{}, "")
	if err != nil && !WalletPaymentReceiptHeld(err) {
		return true, err
	}
	_, err = model.ScheduleWalletTopUpCheck(model.WalletTopUpScope{TenantID: order.TenantID, UserID: order.UserId}, order.TradeNo)
	return true, err
}
func WalletWaffoClient(snapshot WalletGatewaySnapshot) (*waffo.Waffo, error) {
	env := config.Production
	if snapshot.Environment == "test" {
		env = config.Sandbox
	}
	cfg, err := config.NewConfigBuilder().APIKey(snapshot.APIKey).PrivateKey(snapshot.PrivateKey).WaffoPublicKey(snapshot.PublicKey).Environment(env).MerchantID(snapshot.MerchantID).Build()
	if err != nil {
		return nil, err
	}
	return waffo.New(cfg), nil
}
func WalletPaymentMinor(amount string, exponent int) (int64, error) {
	if amount == "" || len(amount) > 32 || strings.TrimSpace(amount) != amount || strings.ContainsAny(amount, "eE+-") || exponent < 0 || exponent > 3 {
		return 0, model.ErrWalletPaymentEvidence
	}
	value, err := decimal.NewFromString(amount)
	if err != nil || value.IsNegative() || value.Exponent() < -int32(exponent) {
		return 0, model.ErrWalletPaymentEvidence
	}
	minor := value.Shift(int32(exponent))
	if minor.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return 0, model.ErrWalletPaymentEvidence
	}
	return minor.IntPart(), nil
}
func WalletPaymentTimestamp(value string) int64 {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000Z"} {
		parsed, err := time.Parse(layout, value)
		if err == nil && parsed.Unix() > 0 && parsed.Unix() <= time.Now().Unix() {
			return parsed.Unix()
		}
	}
	return 0
}

// The verified typed Pancake payload exposes actual payment and tax totals.
// Missing payment fields are retained as reviewable proof, never inferred from
// the ambiguous order amount field.
func HandleWalletPancakeCallback(event *WaffoPancakeWebhookEvent) (bool, error) {
	if event == nil {
		return false, model.ErrWalletPaymentEvidence
	}
	if !strings.HasPrefix(event.Data.OrderMerchantExternalID, "WT") {
		return false, nil
	}
	order, snapshot, err := WalletGatewayForOrder(event.Data.OrderMerchantExternalID, model.PaymentProviderWaffoPancake)
	if WalletOrderNotFound(err) {
		if setting.WaffoPancakeStoreID == "" || event.StoreID != setting.WaffoPancakeStoreID || setting.WaffoPancakeMerchantID == "" {
			return true, model.ErrWalletPaymentEvidence
		}
		if event.Mode != "test" && event.Mode != "prod" {
			return true, model.ErrWalletPaymentEvidence
		}
		minor, e := WalletPaymentMinor(event.Data.Total, 2)
		if e != nil || event.Data.PaymentID == "" {
			return true, model.ErrWalletPaymentEvidence
		}
		err := SaveUnmatchedWalletPaymentReceipt(context.Background(), model.WalletPaymentReceipt{Provider: model.PaymentProviderWaffoPancake, ProviderAccountRef: "pancake:" + setting.WaffoPancakeMerchantID, ProviderEnvironment: event.Mode, TransactionID: event.Data.PaymentID, AmountMinor: minor, Currency: strings.ToUpper(event.Data.Currency), CurrencyExponent: 2, PaidAt: WalletPaymentTimestamp(event.Data.PaymentDate), EvidenceSource: "verified_callback", EvidenceRef: "pancake-event:" + event.ID})
		return true, err
	}
	if err != nil {
		return order != nil && order.WalletLifecycleVersion == 1, err
	}
	if snapshot == nil {
		return false, nil
	}
	if event.Mode != order.ProviderEnvironment || snapshot.StoreID == "" || event.StoreID != snapshot.StoreID || event.Data.MerchantProvidedBuyerIdentity != snapshot.BuyerIdentity || event.Data.OrderID == "" || strings.ToUpper(event.Data.Currency) != order.Currency {
		return true, model.ErrWalletPaymentEvidence
	}
	transaction := event.Data.PaymentID
	if transaction == "" {
		transaction = "order:" + event.Data.OrderID
	}
	total, amountErr := WalletPaymentMinor(event.Data.Total, order.CurrencyExponent)
	if amountErr != nil {
		total, amountErr = WalletPaymentMinor(event.Data.Amount, order.CurrencyExponent)
	}
	if amountErr != nil {
		return true, amountErr
	}
	subtotal, subtotalErr := WalletPaymentMinor(event.Data.Subtotal, order.CurrencyExponent)
	tax, taxErr := WalletPaymentMinor(event.Data.TaxAmount, order.CurrencyExponent)
	pricing := subtotalErr == nil && taxErr == nil && subtotal == order.PaidMinor && tax <= math.MaxInt64-subtotal && total == subtotal+tax && event.Data.Total != "" && event.Data.PaymentID != "" && strings.EqualFold(event.Data.PaymentStatus, "succeeded")
	_, err = FinalizeWalletPaymentReceipt(order, transaction, total, order.Currency, order.CurrencyExponent, WalletPaymentTimestamp(event.Data.PaymentDate), "verified_callback", "pancake-event:"+event.ID, pricing, model.TopUpUserPatch{}, "")
	if WalletPaymentReceiptHeld(err) {
		return true, nil
	}
	return true, err
}

// SaveUnmatchedWalletPaymentReceipt is used only after authenticating the
// current merchant's callback when no local wallet order can be located.
func SaveUnmatchedWalletPaymentReceipt(ctx context.Context, receipt model.WalletPaymentReceipt) error {
	if err := CheckWalletPaymentRuntimeSchema(ctx, model.DB); err != nil {
		return err
	}
	receipt.TopUpID = nil
	receipt.VerificationStatus = "verified"
	receipt.PricingVerified = false
	_, err := model.SaveWalletPaymentReceipt(&receipt)
	return err
}
func WalletOrderNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, model.ErrTopUpNotFound)
}

func WalletPaymentReceiptHeld(err error) bool {
	return errors.Is(err, model.ErrWalletPaymentEvidence) || errors.Is(err, model.ErrWalletPaymentProofIncomplete) || errors.Is(err, model.ErrTopUpQuotaLimitExceeded)
}
