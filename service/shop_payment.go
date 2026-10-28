package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"gorm.io/gorm"
)

var ErrShopChannelUnavailable = errors.New("SHOP_CHANNEL_UNAVAILABLE")

// ShopPaymentNotCommittedError marks a domain rejection inside the creation
// transaction. Transport/commit errors and errors after that transaction must
// never acquire this marker: the client must keep the original request key.
type ShopPaymentNotCommittedError struct{ Cause error }

func (e *ShopPaymentNotCommittedError) Error() string { return e.Cause.Error() }
func (e *ShopPaymentNotCommittedError) Unwrap() error { return e.Cause }

type ShopPaymentMethod struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Icon string `json:"icon,omitempty"`
}
type ShopCheckout struct {
	Kind string `json:"kind"`
	URL  string `json:"url"`
}
type ShopPaymentResult struct {
	Attempt  *model.ShopPaymentAttempt
	Checkout *ShopCheckout
}

// This snapshot is encrypted at rest and is never a response DTO. Existing
// liabilities keep their original merchant credentials when settings change.
type shopEpaySnapshot struct {
	APIURL      string `json:"api_url"`
	MerchantID  string `json:"merchant_id"`
	Key         string `json:"key"`
	AccountRef  string `json:"account_ref"`
	Environment string `json:"environment"`
	Method      string `json:"method"`
	NotifyURL   string `json:"notify_url"`
	ReturnURL   string `json:"return_url"`
}

func shopHTTPS(value string) (*url.URL, error) {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, ErrShopChannelUnavailable
	}
	return u, nil
}

func ShopPaymentReady(settings *model.ShopSettings) bool {
	if settings == nil || settings.Provider != "epay" || settings.ProviderAccountRef != "epay:"+operation_setting.EpayId || operation_setting.EpayId == "" || strings.TrimSpace(operation_setting.EpayKey) == "" || settings.Environment != "production" && settings.Environment != "test" || settings.Currency != "CNY" || settings.CurrencyExponent != 2 || settings.MinAmountMinor <= 0 || settings.MaxAmountMinor < settings.MinAmountMinor || !ShopSecretsReady() {
		return false
	}
	if _, err := shopHTTPS(operation_setting.PayAddress); err != nil {
		return false
	}
	base := operation_setting.CustomCallbackAddress
	if base == "" {
		base = system_setting.ServerAddress
	}
	if _, err := shopHTTPS(base); err != nil {
		return false
	}
	return len(ShopConfiguredPaymentMethods()) > 0
}

// ShopConfiguredPaymentMethods projects the shared channel configuration for
// administrators even while shop sales or shop credentials are not ready.
func ShopConfiguredPaymentMethods() []ShopPaymentMethod {
	methods := []ShopPaymentMethod{}
	seen := map[string]bool{}
	for _, m := range operation_setting.PayMethods {
		if m["type"] != "" && shopProofID(m["type"]) && len(m["type"]) <= 32 && !seen[m["type"]] {
			methods = append(methods, ShopPaymentMethod{ID: m["type"], Name: m["name"], Icon: m["icon"]})
			seen[m["type"]] = true
		}
	}
	return methods
}

func ShopPaymentMethods(db *gorm.DB) ([]ShopPaymentMethod, error) {
	settings, err := model.GetShopSettings(db)
	if err != nil {
		return nil, err
	}
	if !ShopPaymentReady(settings) {
		return []ShopPaymentMethod{}, nil
	}
	return ShopConfiguredPaymentMethods(), nil
}

func shopPaymentBinding(purpose, attemptID string) ShopSecretBinding {
	return ShopSecretBinding{Purpose: purpose, TenantID: model.PlatformTenantID, ObjectID: attemptID, TemplateVersion: "1"}
}

func openShopEpaySnapshot(attempt *model.ShopPaymentAttempt) (shopEpaySnapshot, error) {
	var snapshot shopEpaySnapshot
	err := OpenShopSecret(model.ShopSecret{Ciphertext: attempt.ProviderSnapshotCiphertext, KeyVersion: attempt.CredentialKeyVersion}, shopPaymentBinding("payment_snapshot", attempt.PublicID), &snapshot)
	if err != nil {
		return snapshot, err
	}
	if attempt.Provider != "epay" || attempt.BusinessKind != "shop" || snapshot.AccountRef != attempt.ProviderAccountRef || snapshot.AccountRef != "epay:"+snapshot.MerchantID || snapshot.Environment != attempt.Environment || snapshot.Method != attempt.PaymentMethod || snapshot.Key == "" {
		return shopEpaySnapshot{}, ErrShopChannelUnavailable
	}
	if _, err = shopHTTPS(snapshot.APIURL); err != nil {
		return shopEpaySnapshot{}, err
	}
	return snapshot, nil
}

func CreateShopPayment(ctx context.Context, db *gorm.DB, c model.ShopCommand, orderID, method string) (*ShopPaymentResult, error) {
	db = db.WithContext(ctx)
	settings, err := model.GetShopSettings(db)
	if err != nil {
		return nil, err
	}
	publicID, err := model.NewShopPublicID("sp_")
	if err != nil {
		return nil, err
	}
	trade, err := model.NewShopPublicID("KS")
	if err != nil {
		return nil, err
	}
	var sealed model.ShopSecret
	if ShopPaymentReady(settings) && operation_setting.ContainsPayMethod(method) && len(method) <= 32 && shopProofID(method) {
		base := operation_setting.CustomCallbackAddress
		if base == "" {
			base = system_setting.ServerAddress
		}
		snapshot := shopEpaySnapshot{APIURL: operation_setting.PayAddress, MerchantID: operation_setting.EpayId, Key: operation_setting.EpayKey, AccountRef: settings.ProviderAccountRef, Environment: settings.Environment, Method: method, NotifyURL: strings.TrimRight(base, "/") + "/api/shop/payments/epay/notify", ReturnURL: strings.TrimRight(base, "/") + "/shop/orders/" + url.PathEscape(orderID)}
		sealed, err = SealShopSecret(snapshot, shopPaymentBinding("payment_snapshot", publicID))
		if err != nil {
			return nil, err
		}
	}
	// The model returns committed replays and existing active attempts before
	// checking new-sales configuration. Disabling sales never hides old orders.
	attempt, created, err := model.CreateShopPaymentAttempt(db, model.ShopPaymentAttemptInput{Command: c, OrderPublicID: orderID, PublicID: publicID, TradeNo: trade, Provider: settings.Provider, ProviderAccountRef: settings.ProviderAccountRef, Environment: settings.Environment, PaymentMethod: method, ProviderSnapshot: sealed})
	if err != nil {
		if sealed.Ciphertext == "" && (errors.Is(err, model.ErrShopInvalid) || errors.Is(err, model.ErrShopPaused)) {
			return nil, ErrShopChannelUnavailable
		}
		for _, rejected := range []error{model.ErrShopVersion, model.ErrShopInvalid, model.ErrShopPaused, model.ErrShopAction, model.ErrShopStock, model.ErrShopCapacity} {
			if errors.Is(err, rejected) {
				return nil, &ShopPaymentNotCommittedError{Cause: err}
			}
		}
		return nil, err
	}
	if created {
		snapshot, openErr := openShopEpaySnapshot(attempt)
		var checkout *ShopCheckout
		if openErr == nil {
			checkout, openErr = createShopEpayCheckout(snapshot, attempt)
		}
		var encrypted model.ShopSecret
		if openErr == nil {
			encrypted, openErr = SealShopSecret(checkout, shopPaymentBinding("checkout", attempt.PublicID))
		}
		state, reason := "ready", ""
		if openErr != nil {
			state, reason = "unknown", "CHECKOUT_UNAVAILABLE"
		}
		attempt, err = model.SaveShopPaymentSession(db, model.ShopPaymentSessionInput{AttemptID: attempt.ID, ClaimToken: attempt.ClaimToken, ExpectedVersion: attempt.Version, State: state, SessionID: attempt.TradeNo, ProviderOrderID: attempt.TradeNo, Checkout: encrypted, Reason: reason, Now: c.Now})
		if err != nil {
			return nil, err
		}
	}
	result := &ShopPaymentResult{Attempt: attempt}
	order, err := model.GetShopOrder(db, model.ShopScope{UserID: c.ActorID, TenantID: c.TenantID, Admin: c.Admin}, orderID)
	if err != nil {
		return nil, err
	}
	if attempt.CreateState == "ready" && order.PurchaseState == "open" && order.PaymentState != "paid" && !order.RiskHold && order.ReservationExpiresAt > time.Now().Unix() {
		var checkout ShopCheckout
		if err = OpenShopSecret(model.ShopSecret{Ciphertext: attempt.CheckoutCiphertext, KeyVersion: attempt.CheckoutKeyVersion}, shopPaymentBinding("checkout", attempt.PublicID), &checkout); err != nil {
			return nil, err
		}
		// Persisted checkout data is authenticated, but still verify the URL's
		// origin against the original encrypted merchant snapshot before use.
		snapshot, e := openShopEpaySnapshot(attempt)
		if e != nil {
			return nil, e
		}
		base, e := shopHTTPS(snapshot.APIURL)
		if e != nil {
			return nil, e
		}
		target, e := url.Parse(checkout.URL)
		if e != nil || checkout.Kind != "redirect" || target.Scheme != "https" || target.Host != base.Host || target.User != nil {
			return nil, ErrShopChannelUnavailable
		}
		result.Checkout = &checkout
	}
	return result, nil
}

func createShopEpayCheckout(snapshot shopEpaySnapshot, attempt *model.ShopPaymentAttempt) (*ShopCheckout, error) {
	if attempt.Currency != "CNY" || attempt.CurrencyExponent != 2 || attempt.ExpectedAmountMinor <= 0 {
		return nil, ErrShopChannelUnavailable
	}
	client, err := epay.NewClient(&epay.Config{PartnerID: snapshot.MerchantID, Key: snapshot.Key}, snapshot.APIURL)
	if err != nil {
		return nil, ErrShopChannelUnavailable
	}
	notify, err := shopHTTPS(snapshot.NotifyURL)
	if err != nil {
		return nil, err
	}
	ret, err := shopHTTPS(snapshot.ReturnURL)
	if err != nil {
		return nil, err
	}
	amount := fmt.Sprintf("%d.%02d", attempt.ExpectedAmountMinor/100, attempt.ExpectedAmountMinor%100)
	action, fields, err := client.Purchase(&epay.PurchaseArgs{Type: attempt.PaymentMethod, ServiceTradeNo: attempt.TradeNo, Name: "KK shop purchase", Money: amount, Device: epay.PC, NotifyUrl: notify, ReturnUrl: ret})
	if err != nil {
		return nil, ErrShopChannelUnavailable
	}
	u, err := shopHTTPS(action)
	if err != nil {
		return nil, err
	}
	base, err := shopHTTPS(snapshot.APIURL)
	if err != nil || base.Host != u.Host {
		return nil, ErrShopChannelUnavailable
	}
	q := url.Values{}
	for k, v := range fields {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return &ShopCheckout{Kind: "redirect", URL: u.String()}, nil
}

func shopEpayAmount(value string) (int64, error) {
	if len(value) == 0 || len(value) > 32 || strings.ContainsAny(value, "eE+- \t\r\n") {
		return 0, ErrShopChannelUnavailable
	}
	n, err := model.ParseCNYMinor(value)
	if err != nil || n <= 0 {
		return 0, ErrShopChannelUnavailable
	}
	return n, nil
}

func shopEvidenceDigest(value any) string {
	raw, err := common.Marshal(value)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// Verified callback facts are durably saved before ACK. A callback without a
// trustworthy paid timestamp remains reviewable and cannot release goods.
func HandleShopEpayCallback(ctx context.Context, db *gorm.DB, values url.Values) (bool, error) {
	params := map[string]string{}
	size := 0
	for k, v := range values {
		if len(v) != 1 || len(k) > 100 || len(v[0]) > 4096 {
			return false, ErrShopChannelUnavailable
		}
		size += len(k) + len(v[0])
		params[k] = v[0]
	}
	if size > 16384 || !strings.HasPrefix(params["out_trade_no"], "KS") || len(params["out_trade_no"]) > 128 {
		return false, ErrShopChannelUnavailable
	}
	var attempt model.ShopPaymentAttempt
	err := db.WithContext(ctx).Where("trade_no = ? AND provider = ? AND business_kind = ?", params["out_trade_no"], "epay", "shop").First(&attempt).Error
	var snapshot shopEpaySnapshot
	if err == nil {
		snapshot, err = openShopEpaySnapshot(&attempt)
	} else if errors.Is(err, gorm.ErrRecordNotFound) {
		settings, e := model.GetShopSettings(db.WithContext(ctx))
		if e != nil {
			return false, e
		}
		if settings.Provider != "epay" || settings.ProviderAccountRef != "epay:"+operation_setting.EpayId || operation_setting.EpayId == "" || operation_setting.EpayKey == "" || settings.Environment != "production" && settings.Environment != "test" {
			return false, ErrShopChannelUnavailable
		}
		snapshot = shopEpaySnapshot{APIURL: operation_setting.PayAddress, MerchantID: operation_setting.EpayId, Key: operation_setting.EpayKey, AccountRef: settings.ProviderAccountRef, Environment: settings.Environment}
		err = nil
	}
	if err != nil {
		return false, err
	}
	client, err := epay.NewClient(&epay.Config{PartnerID: snapshot.MerchantID, Key: snapshot.Key}, snapshot.APIURL)
	if err != nil {
		return false, ErrShopChannelUnavailable
	}
	verified, err := client.Verify(params)
	if err != nil || !verified.VerifyStatus || params["pid"] != snapshot.MerchantID || verified.TradeStatus != epay.StatusTradeSuccess || params["trade_no"] == "" || len(params["trade_no"]) > 191 || snapshot.Method != "" && params["type"] != snapshot.Method {
		return false, ErrShopChannelUnavailable
	}
	amount, err := shopEpayAmount(params["money"])
	if err != nil {
		return false, err
	}
	currency := "CNY"
	if params["currency"] != "" {
		currency = strings.ToUpper(params["currency"])
	}
	receipt := model.ShopPaymentReceipt{Provider: "epay", ProviderAccountRef: snapshot.AccountRef, Environment: snapshot.Environment, ProviderTransactionID: params["trade_no"], OriginalTradeRef: params["out_trade_no"], ProviderSessionID: params["out_trade_no"], ProviderOrderID: params["out_trade_no"], AmountMinor: amount, Currency: currency, CurrencyExponent: 2, VerificationSource: "provider_webhook", EvidenceRef: "epay:webhook:" + shopEvidenceDigest(params), EvidenceHash: shopEvidenceDigest(params), PricingVerified: false}
	_, err = model.SaveShopVerifiedReceipt(db.WithContext(ctx), model.ShopVerifiedReceiptInput{Receipt: receipt, Verified: true, Now: time.Now().Unix()})
	return err == nil || errors.Is(err, model.ErrShopEvidence), err
}

type shopEpayQuery struct {
	Paid        bool
	NeedsReview bool
	Receipt     model.ShopPaymentReceipt
	EvidenceRef string
}

func queryShopEpay(ctx context.Context, snapshot shopEpaySnapshot, attempt *model.ShopPaymentAttempt, client *http.Client) (shopEpayQuery, error) {
	var result shopEpayQuery
	u, err := shopHTTPS(snapshot.APIURL)
	if err != nil {
		return result, err
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api.php"
	q := url.Values{"act": {"order"}, "pid": {snapshot.MerchantID}, "key": {snapshot.Key}, "out_trade_no": {attempt.TradeNo}}
	u.RawQuery = q.Encode()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return result, ErrShopChannelUnavailable
	}
	transport := *client
	transport.Timeout = 10 * time.Second
	transport.Jar = nil
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := transport.Do(req)
	if err != nil {
		return result, ErrShopChannelUnavailable
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
	if err != nil || len(raw) > 64<<10 || resp.StatusCode != http.StatusOK {
		return result, ErrShopChannelUnavailable
	}
	if common.GetJsonType(raw) == "string" {
		var inner string
		if common.Unmarshal(raw, &inner) != nil {
			return result, ErrShopChannelUnavailable
		}
		raw = []byte(inner)
	}
	var data struct {
		Code         common.RawMessage `json:"code"`
		Status       common.RawMessage `json:"status"`
		PID          common.RawMessage `json:"pid"`
		TradeNo      string            `json:"trade_no"`
		OutTradeNo   string            `json:"out_trade_no"`
		Money        string            `json:"money"`
		Currency     string            `json:"currency"`
		EndTime      string            `json:"endtime"`
		RefundMoney  common.RawMessage `json:"refund_money"`
		RefundAmount common.RawMessage `json:"refund_amount"`
		RefundStatus common.RawMessage `json:"refund_status"`
	}
	var unique map[string]common.RawMessage
	if common.UnmarshalStrict(raw, &unique) != nil || common.Unmarshal(raw, &data) != nil {
		return result, ErrShopChannelUnavailable
	}
	// Epay implementations add optional fields, so strict decoding is handled
	// separately below: duplicate keys are rejected while known facts are read.
	if shopEpayScalar(data.Code) != "1" || shopEpayScalar(data.PID) != snapshot.MerchantID || data.OutTradeNo != attempt.TradeNo {
		return result, ErrShopChannelUnavailable
	}
	digest := shopEvidenceDigest(string(raw))
	result.EvidenceRef = "epay:query:" + digest
	status := shopEpayScalar(data.Status)
	if status == "0" {
		return result, nil
	}
	if status != "1" || data.TradeNo == "" || len(data.TradeNo) > 191 {
		return result, ErrShopChannelUnavailable
	}
	amount, err := shopEpayAmount(data.Money)
	if err != nil {
		return result, err
	}
	currency := "CNY"
	if data.Currency != "" {
		currency = strings.ToUpper(data.Currency)
	}
	paidAt := int64(0)
	if paid, e := time.ParseInLocation("2006-01-02 15:04:05", data.EndTime, time.FixedZone("CST", 8*3600)); e == nil && paid.Unix() >= attempt.CreatedAt && paid.Unix() <= time.Now().Unix() {
		paidAt = paid.Unix()
	}
	refund := false
	for _, v := range []common.RawMessage{data.RefundMoney, data.RefundAmount, data.RefundStatus} {
		text := shopEpayScalar(v)
		if text != "" && text != "0" && text != "0.00" && text != "false" && text != "null" {
			refund = true
		}
	}
	result.Paid = true
	result.NeedsReview = refund
	result.Receipt = model.ShopPaymentReceipt{Provider: "epay", ProviderAccountRef: snapshot.AccountRef, Environment: snapshot.Environment, ProviderTransactionID: data.TradeNo, OriginalTradeRef: data.OutTradeNo, ProviderSessionID: attempt.TradeNo, ProviderOrderID: attempt.TradeNo, AmountMinor: amount, Currency: currency, CurrencyExponent: 2, ProviderPaidAt: paidAt, VerificationSource: "provider_query", EvidenceRef: result.EvidenceRef, EvidenceHash: digest, PricingVerified: paidAt > 0 && !refund}
	return result, nil
}

func shopEpayScalar(raw common.RawMessage) string {
	var s string
	if common.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

func ReconcileShopPayment(ctx context.Context, db *gorm.DB, attemptPublicID string) error {
	now := time.Now().Unix()
	attempt, err := model.ClaimShopPaymentCheck(db.WithContext(ctx), attemptPublicID, now)
	if err != nil {
		return err
	}
	snapshot, err := openShopEpaySnapshot(attempt)
	var result shopEpayQuery
	if err == nil {
		result, err = queryShopEpay(ctx, snapshot, attempt, http.DefaultClient)
	}
	if err == nil && result.Paid {
		if result.NeedsReview {
			// A provider refund marker is a reason to hold fulfillment, not proof
			// of a particular refund amount or of a completed refund attempt.
			if holdErr := model.HoldShopPaymentForReview(db.WithContext(ctx), attempt.PublicID, "PROVIDER_ADJUSTMENT_REVIEW", result.EvidenceRef, now); holdErr != nil {
				return holdErr
			}
		}
		_, err = model.SaveShopVerifiedReceipt(db.WithContext(ctx), model.ShopVerifiedReceiptInput{Receipt: result.Receipt, Verified: true, Now: now})
		if err != nil && !errors.Is(err, model.ErrShopEvidence) {
			return err
		}
	}
	state, reason := "unknown", "PAYMENT_QUERY_UNAVAILABLE"
	if err == nil {
		state, reason = "unpaid", "PAYMENT_STILL_PAYABLE"
		if result.Paid {
			state, reason = "unknown", "PAYMENT_RECEIPT_PENDING"
		}
	}
	// Querying an unpaid Epay trade does not close its payment URL. A 404,
	// not-found response or transport failure is likewise never closure proof.
	_, applyErr := model.ApplyShopPaymentCheck(db.WithContext(ctx), model.ShopPaymentCheckInput{AttemptID: attempt.ID, ClaimToken: attempt.ClaimToken, ExpectedVersion: attempt.Version, PaymentState: state, Reason: reason, EvidenceRef: result.EvidenceRef, Now: now})
	if applyErr != nil {
		return applyErr
	}
	if err != nil {
		return ErrShopChannelUnavailable
	}
	return nil
}
