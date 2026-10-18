package controller

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/checkout/session"
)

type membershipPaymentMethod struct {
	Provider string `json:"provider"`
	Method   string `json:"method"`
	Name     string `json:"name"`
}

type membershipCheckout struct {
	PaymentURL string            `json:"payment_url,omitempty"`
	SubmitURL  string            `json:"submit_url,omitempty"`
	FormFields map[string]string `json:"form_fields,omitempty"`
}

func obtainMembershipCheckout(ctx context.Context, order *model.MembershipOrder) (*membershipCheckout, error) {
	claimed, create, err := model.ClaimMembershipCheckout(order.TradeNo, order.UserID)
	if err != nil {
		return nil, err
	}
	*order = *claimed
	if !create {
		var checkout membershipCheckout
		if err := common.UnmarshalJsonStr(order.CheckoutData, &checkout); err != nil {
			return nil, err
		}
		return &checkout, nil
	}
	checkout, err := createMembershipCheckout(ctx, order)
	var data []byte
	if err == nil {
		data, err = common.Marshal(checkout)
	}
	if err == nil {
		err = model.SaveMembershipCheckout(order.TradeNo, string(data))
	}
	if err != nil {
		// Remote providers may have created a payable session even when their
		// response or our persistence failed, so their claims must be retained.
		if order.Provider == model.PaymentProviderEpay {
			// Purchase only signs a local form; no provider session can exist yet.
			// Release this claim so corrected configuration can be retried.
			err = errors.Join(err, model.ReleaseMembershipEpayCheckout(order.TradeNo, order.CheckoutStartedAt))
		}
		return nil, err
	}
	order.CheckoutState, order.CheckoutData = "ready", string(data)
	return checkout, nil
}

func membershipPaymentMethods(tier model.MembershipTier) []membershipPaymentMethod {
	methods := make([]membershipPaymentMethod, 0)
	if !operation_setting.IsPaymentComplianceConfirmed() {
		return methods
	}
	if tier.Currency == "CNY" && GetEpayClient() != nil {
		for _, method := range operation_setting.PayMethods {
			kind := method["type"]
			if kind == "" || kind == model.PaymentMethodStripe || kind == model.PaymentMethodCreem || kind == model.PaymentMethodWaffo || kind == model.PaymentMethodWaffoPancake || kind == model.PaymentMethodBalance {
				continue
			}
			methods = append(methods, membershipPaymentMethod{Provider: model.PaymentProviderEpay, Method: kind, Name: method["name"]})
		}
	}
	if (strings.HasPrefix(setting.StripeApiSecret, "sk_") || strings.HasPrefix(setting.StripeApiSecret, "rk_")) && setting.StripeWebhookSecret != "" {
		methods = append(methods, membershipPaymentMethod{Provider: model.PaymentProviderStripe, Method: model.PaymentMethodStripe, Name: "Stripe"})
	}
	if tier.PaymentProducts[model.PaymentProviderCreem] != "" && setting.CreemApiKey != "" && setting.CreemWebhookSecret != "" {
		methods = append(methods, membershipPaymentMethod{Provider: model.PaymentProviderCreem, Method: model.PaymentMethodCreem, Name: "Creem"})
	}
	if tier.Currency == "USD" && tier.PaymentProducts[model.PaymentProviderWaffoPancake] != "" && setting.WaffoPancakeMerchantID != "" && setting.WaffoPancakePrivateKey != "" && setting.WaffoPancakeStoreID != "" {
		methods = append(methods, membershipPaymentMethod{Provider: model.PaymentProviderWaffoPancake, Method: model.PaymentMethodWaffoPancake, Name: "Waffo Pancake"})
	}
	return methods
}

func createMembershipCheckout(ctx context.Context, order *model.MembershipOrder) (*membershipCheckout, error) {
	if order.Status != model.MembershipOrderPending || order.ExpiresAt <= time.Now().Unix() {
		return nil, model.ErrMembershipConflict
	}
	var terms model.MembershipOrderTerms
	if err := common.UnmarshalJsonStr(order.Terms, &terms); err != nil {
		return nil, err
	}
	user, err := model.GetUserById(order.UserID, false)
	if err != nil {
		return nil, err
	}
	returnURL := paymentReturnPath("/membership?order=" + url.QueryEscape(order.TradeNo))
	switch order.Provider {
	case model.PaymentProviderEpay:
		if order.Currency != "CNY" {
			return nil, errors.New("Epay membership payments require CNY")
		}
		client := GetEpayClient()
		if client == nil {
			return nil, errors.New("Epay is not configured")
		}
		notifyURL, err := url.Parse(service.GetCallbackAddress() + "/api/membership/epay/notify")
		if err != nil {
			return nil, err
		}
		redirect, err := url.Parse(returnURL)
		if err != nil {
			return nil, err
		}
		name := "Membership: " + strings.ReplaceAll(terms.Tier.Name, "&", "＆")
		uri, fields, err := client.Purchase(&epay.PurchaseArgs{Type: order.PaymentMethod, ServiceTradeNo: order.TradeNo, Name: name, Money: decimal.NewFromInt(order.AmountMinor).Div(decimal.NewFromInt(100)).StringFixed(2), Device: epay.PC, NotifyUrl: notifyURL, ReturnUrl: redirect})
		if err != nil {
			return nil, err
		}
		return &membershipCheckout{SubmitURL: uri, FormFields: fields}, nil
	case model.PaymentProviderStripe:
		params := &stripe.CheckoutSessionParams{
			ClientReferenceID: stripe.String(order.TradeNo), Mode: stripe.String(string(stripe.CheckoutSessionModePayment)),
			SuccessURL: stripe.String(returnURL), CancelURL: stripe.String(returnURL), AllowPromotionCodes: stripe.Bool(false),
			ExpiresAt: stripe.Int64(order.ExpiresAt),
			LineItems: []*stripe.CheckoutSessionLineItemParams{{Quantity: stripe.Int64(1), PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
				Currency: stripe.String(strings.ToLower(order.Currency)), UnitAmount: stripe.Int64(order.AmountMinor),
				ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{Name: stripe.String(terms.Tier.Name)},
			}}},
		}
		params.Context = ctx
		params.SetIdempotencyKey(order.TradeNo)
		if user.StripeCustomer != "" {
			params.Customer = stripe.String(user.StripeCustomer)
		} else if user.Email != "" {
			params.CustomerEmail = stripe.String(user.Email)
		}
		client := session.Client{B: stripe.GetBackend(stripe.APIBackend), Key: setting.StripeApiSecret}
		checkout, err := client.New(params)
		if err != nil {
			return nil, err
		}
		if checkout.URL == "" {
			return nil, errors.New("Stripe returned an empty checkout URL")
		}
		return &membershipCheckout{PaymentURL: checkout.URL}, nil
	case model.PaymentProviderCreem:
		productID := terms.Tier.PaymentProducts[model.PaymentProviderCreem]
		if err := validateMembershipCreemProduct(ctx, productID, order.AmountMinor, order.Currency); err != nil {
			return nil, err
		}
		link, err := genCreemLink(ctx, order.TradeNo, &CreemProduct{ProductId: productID, Name: terms.Tier.Name, Price: decimal.NewFromInt(order.AmountMinor).Div(decimal.NewFromInt(100)).InexactFloat64(), Currency: order.Currency}, user.Email, user.Username)
		if err != nil {
			return nil, err
		}
		return &membershipCheckout{PaymentURL: link}, nil
	case model.PaymentProviderWaffoPancake:
		if order.Currency != "USD" {
			return nil, errors.New("Waffo Pancake membership payments require USD")
		}
		productID := terms.Tier.PaymentProducts[model.PaymentProviderWaffoPancake]
		catalog, err := service.ListWaffoPancakeCatalog(ctx, setting.WaffoPancakeMerchantID, setting.WaffoPancakePrivateKey)
		if err != nil {
			return nil, err
		}
		found := false
		for _, store := range catalog.Stores {
			if store.ID != setting.WaffoPancakeStoreID {
				continue
			}
			for _, product := range store.OnetimeProducts {
				if product.ID == productID {
					found = true
				}
			}
		}
		if !found {
			return nil, errors.New("Pancake product must be an active one-time product in the configured store")
		}
		expires := int(order.ExpiresAt - time.Now().Unix())
		checkout, err := service.CreateWaffoPancakeCheckoutSession(ctx, &service.WaffoPancakeCreateSessionParams{
			ProductID: productID, BuyerIdentity: service.WaffoPancakeBuyerIdentityFromUserID(user.Id), BuyerEmail: getWaffoPancakeBuyerEmail(user),
			PriceSnapshot: &service.WaffoPancakePriceSnapshot{Amount: decimal.NewFromInt(order.AmountMinor).Div(decimal.NewFromInt(100)).StringFixed(2), TaxCategory: "saas"}, ExpiresInSeconds: &expires, OrderMerchantExternalID: order.TradeNo,
		})
		if err != nil {
			return nil, err
		}
		return &membershipCheckout{PaymentURL: checkout.CheckoutURL}, nil
	default:
		return nil, errors.New("unsupported membership payment provider")
	}
}

func validateMembershipCreemProduct(ctx context.Context, productID string, amount int64, currency string) error {
	if productID == "" {
		return errors.New("Creem product is not configured")
	}
	base := "https://api.creem.io"
	if setting.CreemTestMode {
		base = "https://test-api.creem.io"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/products?product_id="+url.QueryEscape(productID), nil)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", setting.CreemApiKey)
	response, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("Creem product verification failed")
	}
	var product struct {
		ID          string `json:"id"`
		Price       int64  `json:"price"`
		Currency    string `json:"currency"`
		BillingType string `json:"billing_type"`
	}
	if err := common.DecodeJson(io.LimitReader(response.Body, 1<<20), &product); err != nil {
		return err
	}
	if product.ID != productID || product.BillingType != "onetime" || product.Price != amount || !strings.EqualFold(product.Currency, currency) {
		return errors.New("Creem product must be one-time and match the frozen membership price and currency")
	}
	return nil
}

func ResumeMembershipPayment(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}
	order, err := model.GetMembershipOrder(c.Param("trade_no"), c.GetInt("id"))
	if err != nil {
		membershipAPIError(c, err)
		return
	}
	checkout, err := obtainMembershipCheckout(c.Request.Context(), order)
	if err != nil {
		membershipAPIError(c, err)
		return
	}
	view, err := membershipOrderResponse(order)
	if err != nil {
		membershipAPIError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"order": view, "trade_no": order.TradeNo, "status": order.Status, "payment_url": checkout.PaymentURL, "submit_url": checkout.SubmitURL, "form_fields": checkout.FormFields})
}

func membershipMoneyMinor(value string) (int64, error) {
	money, err := decimal.NewFromString(value)
	if err != nil {
		return 0, err
	}
	minor := money.Mul(decimal.NewFromInt(100))
	if !minor.IsPositive() || !minor.Equal(minor.Truncate(0)) || minor.GreaterThan(decimal.NewFromInt(99999999999999)) {
		return 0, errors.New("invalid payment amount")
	}
	return minor.IntPart(), nil
}

func MembershipEpayNotify(c *gin.Context) {
	if err := c.Request.ParseForm(); err != nil {
		c.String(http.StatusOK, "fail")
		return
	}
	values := c.Request.URL.Query()
	if c.Request.Method == http.MethodPost {
		values = c.Request.PostForm
	}
	params := make(map[string]string, len(values))
	for key, values := range values {
		if len(values) != 1 {
			c.String(http.StatusOK, "fail")
			return
		}
		params[key] = values[0]
	}
	client := GetEpayClient()
	if client == nil {
		c.String(http.StatusOK, "fail")
		return
	}
	verified, err := client.Verify(params)
	if err != nil || !verified.VerifyStatus || params["pid"] != operation_setting.EpayId || !strings.HasPrefix(verified.ServiceTradeNo, "MEMBER-") {
		c.String(http.StatusOK, "fail")
		return
	}
	if verified.TradeStatus != epay.StatusTradeSuccess {
		c.String(http.StatusOK, "success")
		return
	}
	amount, amountErr := membershipMoneyMinor(verified.Money)
	validation := ""
	if amountErr != nil {
		validation = "invalid provider amount"
	}
	order, err := model.GetMembershipOrder(verified.ServiceTradeNo, 0)
	if err != nil {
		c.String(http.StatusOK, "fail")
		return
	}
	if verified.Type != order.PaymentMethod {
		validation = "payment method mismatch"
	}
	_, err = model.CompleteMembershipPayment(model.MembershipPaymentInput{TradeNo: verified.ServiceTradeNo, Provider: model.PaymentProviderEpay, ReceiptID: verified.TradeNo, AmountMinor: amount, Currency: "CNY", OneTime: true, Paid: true, ValidationError: validation})
	if err != nil {
		c.String(http.StatusOK, "fail")
		return
	}
	c.String(http.StatusOK, "success")
}

func completeMembershipStripe(event stripe.Event) error {
	tradeNo := event.GetObjectValue("client_reference_id")
	amount, err := strconv.ParseInt(event.GetObjectValue("amount_total"), 10, 64)
	validation := ""
	if err != nil || amount <= 0 {
		validation = "invalid provider amount"
	}
	receipt := event.GetObjectValue("payment_intent")
	if receipt == "" {
		receipt = event.GetObjectValue("id")
	}
	_, err = model.CompleteMembershipPayment(model.MembershipPaymentInput{
		TradeNo: tradeNo, Provider: model.PaymentProviderStripe, ReceiptID: receipt, AmountMinor: amount, Currency: strings.ToUpper(event.GetObjectValue("currency")),
		OneTime: event.GetObjectValue("mode") == string(stripe.CheckoutSessionModePayment), Paid: event.GetObjectValue("payment_status") == "paid", ValidationError: validation,
	})
	return err
}

func completeMembershipCreem(event *CreemWebhookEvent) error {
	validation := ""
	if event.Object.Product.BillingType != "onetime" {
		validation = "product is not one-time"
	}
	_, err := model.CompleteMembershipPayment(model.MembershipPaymentInput{
		TradeNo: event.Object.RequestId, Provider: model.PaymentProviderCreem, ReceiptID: event.Object.Order.Id,
		AmountMinor: int64(event.Object.Order.AmountPaid), Currency: strings.ToUpper(event.Object.Order.Currency),
		OneTime: event.Object.Order.Type == "onetime", Paid: event.Object.Order.Status == "paid", ValidationError: validation,
	})
	return err
}

func completeMembershipPancake(event *service.WaffoPancakeWebhookEvent) error {
	order, err := model.GetMembershipOrder(event.Data.OrderMerchantExternalID, 0)
	if err != nil {
		return err
	}
	validation := ""
	if event.Data.MerchantProvidedBuyerIdentity != service.WaffoPancakeBuyerIdentityFromUserID(order.UserID) {
		validation = "buyer identity mismatch"
	}
	if event.StoreID != setting.WaffoPancakeStoreID {
		validation = "store identity mismatch"
	}
	amount, err := membershipMoneyMinor(event.Data.Amount)
	if err != nil {
		validation = "invalid provider amount"
	}
	_, err = model.CompleteMembershipPayment(model.MembershipPaymentInput{
		TradeNo: order.TradeNo, Provider: model.PaymentProviderWaffoPancake, ReceiptID: event.Data.OrderID, AmountMinor: amount, Currency: strings.ToUpper(event.Data.Currency),
		OneTime: true, Paid: event.NormalizedEventType() == "order.completed", ValidationError: validation,
	})
	return err
}

// Membership callbacks remain available when sales are paused. Authenticated
// provider facts, including late payments, must still reach the durable ledger.
func membershipCallbackConfigured(provider string) bool {
	switch provider {
	case model.PaymentProviderStripe:
		return setting.StripeWebhookSecret != "" && setting.StripeApiSecret != ""
	case model.PaymentProviderCreem:
		return setting.CreemWebhookSecret != "" && setting.CreemApiKey != ""
	case model.PaymentProviderWaffoPancake:
		return setting.WaffoPancakeMerchantID != "" && setting.WaffoPancakePrivateKey != ""
	default:
		return false
	}
}

func handleMembershipStripeEvent(event stripe.Event) error {
	tradeNo := event.GetObjectValue("client_reference_id")
	switch event.Type {
	case stripe.EventTypeCheckoutSessionCompleted, stripe.EventTypeCheckoutSessionAsyncPaymentSucceeded:
		if event.GetObjectValue("payment_status") != "paid" {
			return nil
		}
		return completeMembershipStripe(event)
	case stripe.EventTypeCheckoutSessionExpired, stripe.EventTypeCheckoutSessionAsyncPaymentFailed:
		order, err := model.GetMembershipOrder(tradeNo, 0)
		if err != nil {
			return err
		}
		if order.Provider != model.PaymentProviderStripe {
			return model.ErrPaymentMethodMismatch
		}
		_, err = model.CloseMembershipOrder(tradeNo, 0)
		return err
	default:
		return nil
	}
}
