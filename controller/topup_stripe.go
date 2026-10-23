package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/checkout/session"
	"github.com/stripe/stripe-go/v81/webhook"
)

var stripeAdaptor = &StripeAdaptor{}

// StripePayRequest represents a payment request for Stripe checkout.
type StripePayRequest struct {
	// Amount is the quantity of units to purchase.
	Amount int64 `json:"amount"`
	// PaymentMethod specifies the payment method (e.g., "stripe").
	PaymentMethod string `json:"payment_method"`
	// SuccessURL is the optional custom URL to redirect after successful payment.
	// If empty, defaults to the server's console log page.
	SuccessURL string `json:"success_url,omitempty"`
	// CancelURL is the optional custom URL to redirect when payment is canceled.
	// If empty, defaults to the server's console topup page.
	CancelURL string `json:"cancel_url,omitempty"`
}

type StripeAdaptor struct {
}

func (*StripeAdaptor) RequestAmount(c *gin.Context, req *StripePayRequest) {
	if req.Amount < getStripeMinTopup() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", getStripeMinTopup())})
		return
	}
	if req.Amount > 10000 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值数量不能大于 10000"})
		return
	}
	id := c.GetInt("id")
	group, err := model.GetUserGroup(id, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}
	if rejectInvalidCreditedQuota(c, id, getStripeCreditedQuota(req.Amount, group)) {
		return
	}
	payMoney := getStripePayMoney(float64(req.Amount), group)
	if payMoney <= 0.01 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": strconv.FormatFloat(payMoney, 'f', 2, 64)})
}

func (*StripeAdaptor) RequestPay(c *gin.Context, req *StripePayRequest) {
	walletLegacyCreate(c, dto.WalletQuoteRequest{PaymentProvider: model.PaymentProviderStripe, PaymentMethod: req.PaymentMethod, Amount: strconv.FormatInt(req.Amount, 10)})
}

func RequestStripeAmount(c *gin.Context) {
	var req StripePayRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	stripeAdaptor.RequestAmount(c, &req)
}

func RequestStripePay(c *gin.Context) {
	if walletLegacyPurchase(c, model.PaymentProviderStripe, requestLegacyStripe) {
		return
	}
	var req StripePayRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	stripeAdaptor.RequestPay(c, &req)
}

func StripeWebhook(c *gin.Context) {
	ctx := c.Request.Context()

	payload, err := io.ReadAll(io.LimitReader(c.Request.Body, 1048577))
	if err != nil || len(payload) > 1048576 {
		logger.LogError(ctx, fmt.Sprintf("Stripe webhook 读取请求体失败 path=%q client_ip=%s error=%q", c.Request.URL.Path, c.ClientIP(), "body unavailable"))
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}

	// The untrusted reference only selects the frozen verification key. No
	// payment state is read from this event until the full body verifies.
	var untrusted stripe.Event
	if common.Unmarshal(payload, &untrusted) != nil {
		c.AbortWithStatus(400)
		return
	}
	unmatched := false
	secret := setting.StripeWebhookSecret
	if strings.HasPrefix(untrusted.GetObjectValue("client_reference_id"), "WT") {
		original, err := service.WalletPaymentWebhookSecret(untrusted.GetObjectValue("client_reference_id"), model.PaymentProviderStripe)
		if err != nil {
			if !service.WalletOrderNotFound(err) {
				c.AbortWithStatus(503)
				return
			}
			unmatched = true
		} else {
			secret = original
		}
	}
	if strings.TrimSpace(secret) == "" {
		c.AbortWithStatus(403)
		return
	}
	signature := c.GetHeader("Stripe-Signature")
	event, err := webhook.ConstructEventWithOptions(payload, signature, secret, webhook.ConstructEventOptions{
		IgnoreAPIVersionMismatch: true,
	})

	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("Stripe webhook 验签失败 path=%q client_ip=%s error=%q", c.Request.URL.Path, c.ClientIP(), err.Error()))
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	if unmatched {
		if event.Type != stripe.EventTypeCheckoutSessionCompleted && event.Type != stripe.EventTypeCheckoutSessionAsyncPaymentSucceeded {
			c.Status(200)
			return
		}
		if event.GetObjectValue("payment_status") != "paid" {
			c.Status(200)
			return
		}
		var paid stripe.CheckoutSession
		if event.Data == nil || common.Unmarshal(event.Data.Raw, &paid) != nil || paid.PaymentIntent == nil || paid.PaymentIntent.ID == "" {
			c.AbortWithStatus(400)
			return
		}
		account, err := service.WalletStripeAccountID(ctx, setting.StripeApiSecret)
		if err != nil || event.Account != "" && event.Account != account {
			c.AbortWithStatus(503)
			return
		}
		currency := strings.ToUpper(string(paid.Currency))
		exponent, err := walletCurrencyExponent(currency)
		if err != nil {
			c.AbortWithStatus(400)
			return
		}
		env := "test"
		if paid.Livemode {
			env = "prod"
		}
		if err := service.SaveUnmatchedWalletPaymentReceipt(ctx, model.WalletPaymentReceipt{Provider: model.PaymentProviderStripe, ProviderAccountRef: account, ProviderEnvironment: env, TransactionID: paid.PaymentIntent.ID, AmountMinor: paid.AmountTotal, Currency: currency, CurrencyExponent: exponent, PaidAt: 0, EvidenceSource: "verified_callback", EvidenceRef: "stripe-event:" + event.ID}); err != nil {
			c.AbortWithStatus(500)
			return
		}
		c.Status(200)
		return
	}
	if strings.HasPrefix(event.GetObjectValue("client_reference_id"), "MEMBER-") {
		if err := handleMembershipStripeEvent(event); err != nil {
			logger.LogError(ctx, "membership Stripe callback failed: "+err.Error())
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusOK)
		return
	}
	callerIp := c.ClientIP()
	logger.LogInfo(ctx, fmt.Sprintf("Stripe webhook 验签成功 event_type=%s client_ip=%s path=%q", string(event.Type), callerIp, c.Request.URL.Path))
	var handlingErr error
	switch event.Type {
	case stripe.EventTypeCheckoutSessionCompleted:
		handlingErr = sessionCompleted(ctx, event, callerIp)
	case stripe.EventTypeCheckoutSessionExpired:
		handlingErr = sessionExpired(ctx, event)
	case stripe.EventTypeCheckoutSessionAsyncPaymentSucceeded:
		handlingErr = sessionAsyncPaymentSucceeded(ctx, event, callerIp)
	case stripe.EventTypeCheckoutSessionAsyncPaymentFailed:
		handlingErr = sessionAsyncPaymentFailed(ctx, event, callerIp)
	default:
		logger.LogInfo(ctx, fmt.Sprintf("Stripe webhook 忽略事件 event_type=%s client_ip=%s", string(event.Type), callerIp))
	}
	if handlingErr != nil {
		logger.LogError(ctx, fmt.Sprintf("Stripe webhook 处理失败 event_type=%s client_ip=%s error=%q", string(event.Type), callerIp, handlingErr.Error()))
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	c.Status(http.StatusOK)
}

func sessionCompleted(ctx context.Context, event stripe.Event, callerIp string) error {
	customerId := event.GetObjectValue("customer")
	referenceId := event.GetObjectValue("client_reference_id")
	status := event.GetObjectValue("status")
	if "complete" != status {
		logger.LogWarn(ctx, fmt.Sprintf("Stripe checkout.completed 状态异常，忽略处理 trade_no=%s status=%s client_ip=%s", referenceId, status, callerIp))
		return nil
	}

	paymentStatus := event.GetObjectValue("payment_status")
	if paymentStatus != "paid" && paymentStatus != "no_payment_required" {
		logger.LogInfo(ctx, fmt.Sprintf("Stripe Checkout 支付未完成，等待异步结果 trade_no=%s payment_status=%s client_ip=%s", referenceId, paymentStatus, callerIp))
		return nil
	}

	return fulfillOrder(ctx, event, referenceId, customerId, callerIp)
}

// sessionAsyncPaymentSucceeded handles delayed payment methods (bank transfer, SEPA, etc.)
// that confirm payment after the checkout session completes.
func sessionAsyncPaymentSucceeded(ctx context.Context, event stripe.Event, callerIp string) error {
	customerId := event.GetObjectValue("customer")
	referenceId := event.GetObjectValue("client_reference_id")
	logger.LogInfo(ctx, fmt.Sprintf("Stripe 异步支付成功 trade_no=%s client_ip=%s", referenceId, callerIp))

	return fulfillOrder(ctx, event, referenceId, customerId, callerIp)
}

// sessionAsyncPaymentFailed marks orders as failed when delayed payment methods
// ultimately fail (e.g. bank transfer not received, SEPA rejected).
func sessionAsyncPaymentFailed(ctx context.Context, event stripe.Event, callerIP string) error {
	reference := event.GetObjectValue("client_reference_id")
	if reference == "" {
		return errors.New("missing order reference")
	}
	topUp, err := model.FindTopUpByTradeNo(reference)
	if err != nil {
		return err
	}
	if topUp.WalletLifecycleVersion == 1 {
		return service.WalletStripeFailedEvent(ctx, topUp, event.GetObjectValue("id"), event.ID)
	}
	err = model.UpdatePendingTopUpStatus(reference, model.PaymentProviderStripe, common.TopUpStatusFailed)
	if errors.Is(err, model.ErrTopUpStatusInvalid) {
		return nil
	}
	return err
}

// fulfillOrder is the shared logic for crediting quota after payment is confirmed.
func fulfillOrder(ctx context.Context, event stripe.Event, referenceId string, customerId string, callerIp string) error {
	if len(referenceId) == 0 {
		logger.LogWarn(ctx, fmt.Sprintf("Stripe 完成订单时缺少订单号 client_ip=%s", callerIp))
		return nil
	}

	LockOrder(referenceId)
	defer UnlockOrder(referenceId)
	payload := map[string]any{
		"customer":     customerId,
		"amount_total": event.GetObjectValue("amount_total"),
		"currency":     strings.ToUpper(event.GetObjectValue("currency")),
		"event_type":   string(event.Type),
	}
	if err := model.CompleteSubscriptionOrder(referenceId, common.GetJsonString(payload), model.PaymentProviderStripe, ""); err == nil {
		logger.LogInfo(ctx, fmt.Sprintf("Stripe 订阅订单处理成功 trade_no=%s event_type=%s client_ip=%s", referenceId, string(event.Type), callerIp))
		return nil
	} else if err != nil && !errors.Is(err, model.ErrSubscriptionOrderNotFound) {
		logger.LogError(ctx, fmt.Sprintf("Stripe 订阅订单处理失败 trade_no=%s event_type=%s client_ip=%s error=%q", referenceId, string(event.Type), callerIp, err.Error()))
		return err
	}

	topUp, findErr := model.FindTopUpByTradeNo(referenceId)
	if findErr != nil {
		return findErr
	}
	if topUp.WalletLifecycleVersion == 1 {
		return service.WalletStripePaidEvent(ctx, topUp, event.GetObjectValue("id"), event.Created)
	}
	err := model.Recharge(referenceId, customerId, callerIp)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Stripe 充值处理失败 trade_no=%s event_type=%s client_ip=%s error=%q", referenceId, string(event.Type), callerIp, err.Error()))
		return err
	}

	total, _ := strconv.ParseFloat(event.GetObjectValue("amount_total"), 64)
	currency := strings.ToUpper(event.GetObjectValue("currency"))
	logger.LogInfo(ctx, fmt.Sprintf("Stripe 充值成功 trade_no=%s amount_total=%.2f currency=%s event_type=%s client_ip=%s", referenceId, total/100, currency, string(event.Type), callerIp))
	return nil
}

func sessionExpired(ctx context.Context, event stripe.Event) error {
	reference := event.GetObjectValue("client_reference_id")
	if event.GetObjectValue("status") != "expired" || reference == "" {
		return errors.New("invalid expiration event")
	}
	LockOrder(reference)
	defer UnlockOrder(reference)
	if err := model.ExpireSubscriptionOrder(reference, model.PaymentProviderStripe); err == nil {
		return nil
	} else if !errors.Is(err, model.ErrSubscriptionOrderNotFound) {
		return err
	}
	topUp, err := model.FindTopUpByTradeNo(reference)
	if err != nil {
		return err
	}
	if topUp.WalletLifecycleVersion == 1 {
		_, err = service.ReconcileWalletTopUp(ctx, reference)
		return err
	}
	err = model.UpdatePendingTopUpStatus(reference, model.PaymentProviderStripe, common.TopUpStatusExpired)
	if errors.Is(err, model.ErrTopUpStatusInvalid) {
		return nil
	}
	return err
}

// genStripeLink generates a Stripe Checkout session URL for payment.
// It creates a new checkout session with the specified parameters and returns the payment URL.
//
// Parameters:
//   - referenceId: unique reference identifier for the transaction
//   - customerId: existing Stripe customer ID (empty string if new customer)
//   - email: customer email address for new customer creation
//   - amount: quantity of units to purchase
//   - successURL: custom URL to redirect after successful payment (empty for default)
//   - cancelURL: custom URL to redirect when payment is canceled (empty for default)
//
// Returns the checkout session URL or an error if the session creation fails.
func genStripeLink(referenceId string, customerId string, email string, amount int64, successURL string, cancelURL string) (string, error) {
	if !strings.HasPrefix(setting.StripeApiSecret, "sk_") && !strings.HasPrefix(setting.StripeApiSecret, "rk_") {
		return "", fmt.Errorf("无效的Stripe API密钥")
	}

	stripe.Key = setting.StripeApiSecret

	// Use custom URLs if provided, otherwise use defaults
	if successURL == "" {
		successURL = paymentReturnPath("/console/log")
	}
	if cancelURL == "" {
		cancelURL = paymentReturnPath("/console/topup")
	}

	params := &stripe.CheckoutSessionParams{
		ClientReferenceID: stripe.String(referenceId),
		SuccessURL:        stripe.String(successURL),
		CancelURL:         stripe.String(cancelURL),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(setting.StripePriceId),
				Quantity: stripe.Int64(amount),
			},
		},
		Mode:                stripe.String(string(stripe.CheckoutSessionModePayment)),
		AllowPromotionCodes: stripe.Bool(setting.StripePromotionCodesEnabled && !model.MembershipTopupDiscountsDisabled()),
	}

	if "" == customerId {
		if "" != email {
			params.CustomerEmail = stripe.String(email)
		}

		params.CustomerCreation = stripe.String(string(stripe.CheckoutSessionCustomerCreationAlways))
	} else {
		params.Customer = stripe.String(customerId)
	}

	result, err := session.New(params)
	if err != nil {
		return "", err
	}

	return result.URL, nil
}

func GetChargedAmount(count float64, user model.User) float64 {
	topUpGroupRatio, _ := topUpPromotionFactors(user.Group, 0)

	return count * topUpGroupRatio
}

func getStripeCreditedQuota(amount int64, group string) decimal.Decimal {
	topUpGroupRatio, _ := topUpPromotionFactors(group, amount)
	if !walletPositiveFinite(topUpGroupRatio, common.QuotaPerUnit) {
		return decimal.Zero
	}
	return decimal.NewFromInt(amount).
		Mul(decimal.NewFromFloat(topUpGroupRatio)).
		Mul(decimal.NewFromFloat(common.QuotaPerUnit))
}

func getStripePayMoney(amount float64, group string) float64 {
	originalAmount := amount
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		amount = amount / common.QuotaPerUnit
	}
	// Using float64 for monetary calculations is acceptable here due to the small amounts involved
	topupGroupRatio, discount := topUpPromotionFactors(group, int64(originalAmount))
	payMoney := amount * setting.StripeUnitPrice * topupGroupRatio * discount
	return payMoney
}

func getStripeMinTopup() int64 {
	minTopup := setting.StripeMinTopUp
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		minTopup = minTopup * int(common.QuotaPerUnit)
	}
	return int64(minTopup)
}
