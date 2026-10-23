package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stripe/stripe-go/v81"
)

const WalletTopUpReconcileTaskType = "wallet_topup_reconcile"

type walletTopUpReconcileHandler struct{}

func (walletTopUpReconcileHandler) Type() string { return WalletTopUpReconcileTaskType }
func (walletTopUpReconcileHandler) Enabled() bool {
	return common.CanRunWriteBackgroundJobs() && model.WalletPaymentSchemaAvailable()
}
func (walletTopUpReconcileHandler) Interval() time.Duration { return 15 * time.Second }
func (walletTopUpReconcileHandler) NewPayload() any         { return nil }
func (walletTopUpReconcileHandler) Run(ctx context.Context, task *model.SystemTask, runner string) {
	if !common.CanRunWriteBackgroundJobs() {
		_ = model.FinishSystemTask(task.TaskID, runner, model.SystemTaskStatusFailed, nil, "write background jobs disabled")
		return
	}
	if err := CheckWalletPaymentRuntimeSchema(ctx, model.DB); err != nil {
		_ = model.FinishSystemTask(task.TaskID, runner, model.SystemTaskStatusFailed, nil, "wallet schema unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	var orders []model.TopUp
	err := model.WalletTopUpOnlyQuery(model.DB.Model(&model.TopUp{})).Where("wallet_lifecycle_version = ? AND status <> ? AND next_check_at > ? AND next_check_at <= ? AND COALESCE(check_claim_until,0) <= ?", 1, common.TopUpStatusSuccess, 0, time.Now().Unix(), time.Now().Unix()).Order("next_check_at,id").Limit(100).Find(&orders).Error
	counts := map[string]int{"credited": 0, "closed": 0, "review_required": 0, "retry_failed": 0}
	if err == nil {
		for _, order := range orders {
			if ctx.Err() != nil {
				err = ctx.Err()
				break
			}
			classification, e := ReconcileWalletTopUp(ctx, order.TradeNo)
			if e != nil {
				classification = "retry_failed"
			}
			counts[classification]++
		}
	}
	status := model.SystemTaskStatusSucceeded
	message := ""
	if err != nil {
		status = model.SystemTaskStatusFailed
		message = "wallet reconciliation unavailable"
	}
	_ = model.FinishSystemTask(task.TaskID, runner, status, counts, message)
}
func init() { RegisterSystemTaskHandler(walletTopUpReconcileHandler{}) }

// ReconcileWalletTopUp is the shared bounded operation for regular jobs and an
// authorized historical run. It never requires or changes a browser session.
func ReconcileWalletTopUp(ctx context.Context, tradeNo string) (string, error) {
	if err := CheckWalletPaymentRuntimeSchema(ctx, model.DB); err != nil {
		return "retry_failed", err
	}
	original, err := model.FindTopUpByTradeNo(tradeNo)
	if err != nil {
		return "retry_failed", err
	}
	if original.Status == common.TopUpStatusSuccess {
		return "credited", nil
	}
	if model.WalletTopUpReliableTerminal(original) {
		return "closed", nil
	}
	order, err := model.ClaimWalletTopUpCheck(tradeNo, time.Now().Unix())
	if err != nil {
		return "retry_failed", err
	}
	result := model.WalletTopUpCheckResult{ClaimToken: order.CheckClaimToken, ExpectedVersion: order.Version, EvidenceSource: "local_reconciliation", ProviderPaymentState: "unknown", NextCheckAt: time.Now().Add(time.Duration(1<<min(order.CheckAttempts, 6)) * 5 * time.Second).Unix()}
	localEnded := model.WalletTopUpLocalEnded(order, time.Now().Unix())
	var snapshot WalletGatewaySnapshot
	if order.WalletLifecycleVersion != 1 || OpenWalletPayment(order.CheckoutPayload, WalletPaymentScope(order), &snapshot) != nil {
		result.ReasonCode = "REVIEW_REQUIRED"
		result.NextCheckAt = time.Now().Add(time.Hour).Unix()
		if localEnded {
			result.NextCheckAt = 0
		}
		applied, err := model.ApplyWalletTopUpCheck(tradeNo, result)
		return walletTopUpCheckOutcome(applied, err, "review_required")
	}
	if order.CreateState == "unknown" || order.CreateState == "creating" {
		deadline := model.WalletTopUpDeadline(order)
		if localEnded || order.LocalEndedAt > 0 || deadline > 0 && deadline <= time.Now().Unix() {
			// Ending an attempt cannot create a fresh payable session while
			// trying to recover an uncertain original creation response.
			result.NextCheckAt = 0
			result.ReasonCode = "CREATE_RESULT_UNKNOWN"
			applied, err := model.ApplyWalletTopUpCheck(tradeNo, result)
			return walletTopUpCheckOutcome(applied, err, "closed")
		}
		// Stripe's fixed idempotency key can recover a lost creation response.
		// Stripe retains keys for at least 24h; after that replay could create a
		// second session, so uncertainty must move to operations instead.
		if order.PaymentProvider == model.PaymentProviderStripe && time.Now().Unix()-order.CreateTime < 23*60*60 {
			remote, e := CreateWalletGatewaySession(ctx, order, snapshot)
			if e == nil && remote != nil && ValidWalletCheckout(remote.Checkout) {
				snapshot.Checkout = remote.Checkout
				snapshot.TokenExpiresAt = remote.TokenExpiresAt
				sealed, e := SealWalletPayment(snapshot, WalletPaymentScope(order))
				if e == nil {
					_, e = model.RecoverWalletTopUpSession(tradeNo, order.CheckClaimToken, order.Version, remote.SessionID, remote.OrderID, sealed, remote.ExpiresAt)
				}
				return "retry_failed", e
			}
		}
		if order.CheckAttempts >= 8 {
			result.ReasonCode = "REVIEW_REQUIRED"
			result.NextCheckAt = time.Now().Add(time.Hour).Unix()
		} else {
			result.ReasonCode = "CREATE_RESULT_UNKNOWN"
		}
		applied, err := model.ApplyWalletTopUpCheck(tradeNo, result)
		return walletTopUpCheckOutcome(applied, err, "retry_failed")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	result.EvidenceSource = "authenticated_query"
	switch order.PaymentProvider {
	case model.PaymentProviderStripe:
		remote, e := RetrieveWalletStripeSession(ctx, order, snapshot)
		if e != nil {
			return walletCheckFailed(order, result, e)
		}
		if remote.PaymentStatus == stripe.CheckoutSessionPaymentStatusPaid || remote.PaymentStatus == stripe.CheckoutSessionPaymentStatusNoPaymentRequired {
			return settleWalletStripe(order, snapshot, remote)
		}
		if remote.Status == stripe.CheckoutSessionStatusComplete {
			result.ProviderPaymentState = "processing"
			result.ReasonCode = "PAYMENT_PROCESSING"
			break
		}
		if remote.Status == stripe.CheckoutSessionStatusOpen && (order.CloseRequestedAt > 0 || localEnded) {
			params := &stripe.CheckoutSessionExpireParams{Params: stripe.Params{Context: ctx}}
			params.SetIdempotencyKey("wallet-close-" + order.TradeNo)
			_, e = WalletStripeClient(snapshot.APIKey).Expire(order.ProviderSessionID, params)
			if e != nil {
				return walletCheckFailed(order, result, e)
			}
			remote, e = RetrieveWalletStripeSession(ctx, order, snapshot)
			if e != nil {
				return walletCheckFailed(order, result, e)
			}
			if remote.PaymentStatus == stripe.CheckoutSessionPaymentStatusPaid || remote.PaymentStatus == stripe.CheckoutSessionPaymentStatusNoPaymentRequired {
				return settleWalletStripe(order, snapshot, remote)
			}
		}
		if remote.Status == stripe.CheckoutSessionStatusExpired && remote.PaymentStatus == stripe.CheckoutSessionPaymentStatusUnpaid {
			result.ProviderPaymentState = "unpaid"
			result.ReliableClosed = true
			result.CloseEvidenceRef = "stripe-session:" + remote.ID
			result.TerminalStatus = common.TopUpStatusExpired
			if order.CloseReason == "user_cancel" {
				result.TerminalStatus = "cancelled"
			}
			result.NextCheckAt = 0
		} else if remote.Status == stripe.CheckoutSessionStatusOpen {
			result.ProviderPaymentState = "unpaid"
			result.ReasonCode = ""
		} else {
			result.ProviderPaymentState = "processing"
			result.ReasonCode = "PAYMENT_PROCESSING"
		}
	case model.PaymentProviderEpay:
		// The existing merchant query proves paid facts only. A non-success
		// response or missing order never proves a pending session is closed.
		receipt, paidAt, e := queryInvoiceEpayPayment(ctx, http.DefaultClient, snapshot.APIURL, snapshot.MerchantID, snapshot.APIKey, *order)
		if errors.Is(e, errEpayOrderUnpaid) {
			result.ProviderPaymentState = "unpaid"
			result.ReasonCode = ""
			result.NextCheckAt = time.Now().Add(time.Minute).Unix()
			break
		}
		if e != nil {
			return walletCheckFailed(order, result, e)
		}
		_, e = FinalizeWalletPaymentReceipt(order, receipt.ProviderReceiptID, receipt.AmountMinor, receipt.Currency, 2, paidAt, "merchant_query", "epay-order:"+receipt.ProviderReceiptID, true, model.TopUpUserPatch{}, "")
		if e != nil {
			return "review_required", e
		}
		return "credited", nil
	default:
		result.ReasonCode = "REVIEW_REQUIRED"
		result.NextCheckAt = time.Now().Add(time.Hour).Unix()
	}
	if localEnded && order.CheckAttempts >= 8 {
		result.NextCheckAt = 0
	}
	applied, err := model.ApplyWalletTopUpCheck(tradeNo, result)
	return walletTopUpCheckOutcome(applied, err, "retry_failed")
}

func walletTopUpCheckOutcome(order *model.TopUp, err error, fallback string) (string, error) {
	if err != nil {
		return "retry_failed", err
	}
	// The model can retain a paid receipt for review instead of applying a
	// negative query. Task counters must describe the committed outcome.
	if order.Status == common.TopUpStatusSuccess {
		return "credited", nil
	}
	if order.Status == "credit_review" || order.Status == "review_required" || order.ProviderPaymentState == "paid" {
		return "review_required", nil
	}
	if model.WalletTopUpReliableTerminal(order) {
		return "closed", nil
	}
	return fallback, nil
}

func walletCheckFailed(order *model.TopUp, result model.WalletTopUpCheckResult, cause error) (string, error) {
	if order.CheckAttempts >= 8 {
		result.ReasonCode = "REVIEW_REQUIRED"
		result.NextCheckAt = time.Now().Add(time.Hour).Unix()
		if model.WalletTopUpLocalEnded(order, time.Now().Unix()) {
			result.NextCheckAt = 0
		}
	} else {
		result.ReasonCode = "PAYMENT_PROVIDER_UNAVAILABLE"
	}
	applied, err := model.ApplyWalletTopUpCheck(order.TradeNo, result)
	category, err := walletTopUpCheckOutcome(applied, err, "retry_failed")
	if err != nil || category != "retry_failed" {
		return category, err
	}
	return "retry_failed", cause
}
func RetrieveWalletStripeSession(ctx context.Context, order *model.TopUp, snapshot WalletGatewaySnapshot) (*stripe.CheckoutSession, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	params := &stripe.CheckoutSessionParams{Params: stripe.Params{Context: ctx}}
	params.AddExpand("line_items.data.price")
	params.AddExpand("payment_intent.latest_charge.balance_transaction")
	remote, err := WalletStripeClient(snapshot.APIKey).Get(order.ProviderSessionID, params)
	if err != nil || remote == nil {
		return nil, ErrWalletProviderUnavailable
	}
	if remote.ID != order.ProviderSessionID || remote.ClientReferenceID != order.TradeNo || remote.Mode != "payment" || strings.ToUpper(string(remote.Currency)) != order.Currency || remote.Livemode != (order.ProviderEnvironment == "prod") {
		return nil, model.ErrWalletPaymentEvidence
	}
	return remote, nil
}
func settleWalletStripe(order *model.TopUp, snapshot WalletGatewaySnapshot, remote *stripe.CheckoutSession, verifiedEventTime ...int64) (string, error) {
	transaction := ""
	kind := "payment"
	if remote.PaymentIntent != nil {
		transaction = remote.PaymentIntent.ID
	}
	if remote.PaymentStatus == stripe.CheckoutSessionPaymentStatusNoPaymentRequired && remote.AmountTotal == 0 {
		transaction = "no-payment:" + remote.ID
		kind = "no_payment_required"
	}
	if transaction == "" {
		return "review_required", model.ErrWalletPaymentEvidence
	}
	paidAt := int64(0)
	if remote.PaymentIntent != nil && remote.PaymentIntent.LatestCharge != nil {
		charge := remote.PaymentIntent.LatestCharge
		if charge.Paid && charge.Captured && charge.BalanceTransaction != nil {
			paidAt = charge.BalanceTransaction.Created
		}
	}
	if paidAt <= 0 && len(verifiedEventTime) > 0 {
		paidAt = verifiedEventTime[0]
	}
	if paidAt < order.CreateTime || paidAt > time.Now().Unix() {
		paidAt = 0
	}
	pricingVerified := validWalletStripePricing(order, snapshot, remote)
	patch := model.TopUpUserPatch{}
	if remote.Customer != nil {
		patch.StripeCustomer = &remote.Customer.ID
	}
	_, err := FinalizeWalletPaymentReceipt(order, transaction, remote.AmountTotal, order.Currency, order.CurrencyExponent, paidAt, "merchant_query", "stripe-session:"+remote.ID, pricingVerified, patch, kind)
	if err != nil {
		return "review_required", err
	}
	return "credited", nil
}
func validWalletStripePricing(order *model.TopUp, snapshot WalletGatewaySnapshot, remote *stripe.CheckoutSession) bool {
	if remote.PaymentIntent != nil && remote.PaymentIntent.LatestCharge != nil && (remote.PaymentIntent.LatestCharge.Refunded || remote.PaymentIntent.LatestCharge.AmountRefunded > 0) {
		return false
	}
	if remote.LineItems == nil || remote.LineItems.HasMore || len(remote.LineItems.Data) != 1 || remote.AmountSubtotal != order.PaidMinor || remote.AmountTotal < 0 || remote.Mode != "payment" || remote.AutomaticTax != nil && remote.AutomaticTax.Enabled {
		return false
	}
	line := remote.LineItems.Data[0]
	if line.Price == nil || line.Price.ID != snapshot.PriceID || line.Quantity != snapshot.Quantity || line.AmountSubtotal != order.PaidMinor {
		return false
	}
	discount, tax, shipping := int64(0), int64(0), int64(0)
	if remote.TotalDetails != nil {
		discount = remote.TotalDetails.AmountDiscount
		tax = remote.TotalDetails.AmountTax
		shipping = remote.TotalDetails.AmountShipping
	}
	if discount < 0 || discount > order.PaidMinor || tax != 0 || shipping != 0 || remote.AmountTotal != order.PaidMinor-discount {
		return false
	}
	if discount > 0 && !snapshot.AllowPromotionCodes {
		return false
	}
	if remote.AmountTotal == 0 {
		return snapshot.AllowPromotionCodes && remote.PaymentStatus == stripe.CheckoutSessionPaymentStatusNoPaymentRequired
	}
	return remote.PaymentStatus == stripe.CheckoutSessionPaymentStatusPaid
}
func FinalizeWalletPaymentReceipt(order *model.TopUp, transaction string, minor int64, currency string, exponent int, paidAt int64, source, reference string, pricingVerified bool, patch model.TopUpUserPatch, kind string) (*model.FinalizeTopUpResult, error) {
	if err := CheckWalletPaymentRuntimeSchema(context.Background(), model.DB); err != nil {
		return nil, err
	}
	if kind == "" {
		kind = "payment"
	}
	id := order.Id
	return model.FinalizeVerifiedWalletTopUp(model.FinalizeVerifiedWalletTopUpInput{TradeNo: order.TradeNo, PricingVerified: pricingVerified, UserPatch: patch, Receipt: model.WalletPaymentReceipt{Provider: order.PaymentProvider, ProviderAccountRef: order.ProviderAccountRef, ProviderEnvironment: order.ProviderEnvironment, TransactionID: transaction, TopUpID: &id, AmountMinor: minor, Currency: strings.ToUpper(currency), CurrencyExponent: exponent, PaidAt: paidAt, EvidenceSource: source, EvidenceRef: reference, VerificationStatus: "verified", SettlementKind: kind}})
}
func WalletPaymentWebhookSecret(tradeNo, provider string) (string, error) {
	_, snapshot, err := WalletGatewayForOrder(tradeNo, provider)
	if err != nil {
		return "", err
	}
	if snapshot == nil {
		return "", ErrWalletProviderUnavailable
	}
	return snapshot.WebhookSecret, nil
}
func WalletStripePaidEvent(ctx context.Context, order *model.TopUp, eventSessionID string, verifiedEventTime int64) error {
	if eventSessionID == "" || order.ProviderSessionID != "" && eventSessionID != order.ProviderSessionID {
		return model.ErrWalletPaymentEvidence
	}
	var s WalletGatewaySnapshot
	if OpenWalletPayment(order.CheckoutPayload, WalletPaymentScope(order), &s) != nil {
		return ErrWalletSecretUnavailable
	}
	if order.ProviderSessionID == "" {
		copy := *order
		copy.ProviderSessionID = eventSessionID
		order = &copy
	}
	remote, err := RetrieveWalletStripeSession(ctx, order, s)
	if err != nil {
		return err
	}
	_, err = settleWalletStripe(order, s, remote, verifiedEventTime)
	return err
}
func WalletStripeFailedEvent(ctx context.Context, order *model.TopUp, sessionID, eventID string) error {
	if sessionID == "" || eventID == "" || order.ProviderSessionID != sessionID {
		return model.ErrWalletPaymentEvidence
	}
	var snapshot WalletGatewaySnapshot
	if OpenWalletPayment(order.CheckoutPayload, WalletPaymentScope(order), &snapshot) != nil {
		return ErrWalletSecretUnavailable
	}
	remote, err := RetrieveWalletStripeSession(ctx, order, snapshot)
	if err != nil {
		return err
	}
	if remote.PaymentStatus == stripe.CheckoutSessionPaymentStatusPaid || remote.PaymentStatus == stripe.CheckoutSessionPaymentStatusNoPaymentRequired {
		_, err = settleWalletStripe(order, snapshot, remote)
		return err
	}
	if remote.Status != stripe.CheckoutSessionStatusComplete || remote.PaymentIntent == nil || (remote.PaymentIntent.Status != "requires_payment_method" && remote.PaymentIntent.Status != "canceled") {
		return model.ErrWalletPaymentEvidence
	}
	claimed, err := model.ClaimWalletTopUpCheck(order.TradeNo, time.Now().Unix())
	if err != nil {
		return err
	}
	_, err = model.ApplyWalletTopUpCheck(order.TradeNo, model.WalletTopUpCheckResult{ClaimToken: claimed.CheckClaimToken, ExpectedVersion: claimed.Version, EvidenceSource: "verified_callback", ProviderPaymentState: "failed", ReliableClosed: true, CloseEvidenceRef: "stripe-event:" + eventID, TerminalStatus: common.TopUpStatusFailed})
	return err
}
