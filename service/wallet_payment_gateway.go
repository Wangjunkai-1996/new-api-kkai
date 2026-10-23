package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/checkout/session"
	"github.com/stripe/stripe-go/v81/price"
	waffo "github.com/waffo-com/waffo-go"
	"github.com/waffo-com/waffo-go/config"
	"github.com/waffo-com/waffo-go/types/order"
)

// WalletGatewaySnapshot is stored only inside authenticated encryption. It is
// never a quote-token or response DTO. It also preserves credential versions.
type WalletGatewaySnapshot struct {
	Provider            string
	APIURL              string
	APIKey              string
	PrivateKey          string
	PublicKey           string
	WebhookSecret       string
	StoreID             string
	MerchantID          string
	Environment         string
	PriceID             string
	ProductID           string
	Quantity            int64
	AmountText          string
	Currency            string
	Exponent            int
	AllowPromotionCodes bool
	PayMethodType       string
	PayMethodName       string
	NotifyURL           string
	ReturnURL           string
	CustomerID          string
	Email               string
	Username            string
	BuyerIdentity       string
	CanResume           bool
	Checkout            *dto.WalletCheckout
	TokenExpiresAt      int64
}
type WalletGatewaySession struct {
	SessionID      string
	OrderID        string
	ExpiresAt      int64
	Checkout       *dto.WalletCheckout
	TokenExpiresAt int64
}

var ErrWalletProviderUnavailable = errors.New("payment provider unavailable")
var ErrWalletCreateNotStarted = errors.New("payment creation not started")

func WalletStripePrice(ctx context.Context, secret, priceID string) (*stripe.Price, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	client := price.Client{B: stripe.GetBackend(stripe.APIBackend), Key: secret}
	return client.Get(priceID, &stripe.PriceParams{Params: stripe.Params{Context: ctx}})
}
func WalletStripeAccountID(ctx context.Context, secret string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var account stripe.Account
	err := stripe.GetBackend(stripe.APIBackend).Call(http.MethodGet, "/v1/account", secret, &stripe.Params{Context: ctx}, &account)
	if err != nil || account.ID == "" {
		return "", ErrWalletProviderUnavailable
	}
	return account.ID, nil
}
func WalletStripeClient(secret string) session.Client {
	return session.Client{B: stripe.GetBackend(stripe.APIBackend), Key: secret}
}
func WalletPaymentScope(order *model.TopUp) string {
	return fmt.Sprintf("%d/%d/%s/%s/%s", order.TenantID, order.UserId, order.TradeNo, order.PaymentProvider, order.ProviderAccountRef)
}

func CreateWalletGatewaySession(ctx context.Context, topUp *model.TopUp, snapshot WalletGatewaySnapshot) (*WalletGatewaySession, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	switch snapshot.Provider {
	case model.PaymentProviderStripe:
		params := &stripe.CheckoutSessionParams{Params: stripe.Params{Context: ctx}, ClientReferenceID: stripe.String(topUp.TradeNo), SuccessURL: stripe.String(snapshot.ReturnURL), CancelURL: stripe.String(snapshot.ReturnURL), Mode: stripe.String("payment"), AllowPromotionCodes: stripe.Bool(snapshot.AllowPromotionCodes), LineItems: []*stripe.CheckoutSessionLineItemParams{{Price: stripe.String(snapshot.PriceID), Quantity: stripe.Int64(snapshot.Quantity)}}}
		params.SetIdempotencyKey("wallet-topup-" + topUp.TradeNo)
		if snapshot.CustomerID != "" {
			params.Customer = stripe.String(snapshot.CustomerID)
		} else {
			params.CustomerCreation = stripe.String("always")
			if snapshot.Email != "" {
				params.CustomerEmail = stripe.String(snapshot.Email)
			}
		}
		remote, err := WalletStripeClient(snapshot.APIKey).New(params)
		if err != nil {
			return nil, ErrWalletProviderUnavailable
		}
		if remote.ID == "" || remote.URL == "" || remote.ClientReferenceID != topUp.TradeNo {
			return nil, ErrWalletProviderUnavailable
		}
		return &WalletGatewaySession{SessionID: remote.ID, OrderID: remote.ID, ExpiresAt: remote.ExpiresAt, Checkout: &dto.WalletCheckout{Kind: "url", URL: remote.URL}}, nil
	case model.PaymentProviderEpay:
		client, err := epay.NewClient(&epay.Config{PartnerID: snapshot.MerchantID, Key: snapshot.APIKey}, snapshot.APIURL)
		if err != nil {
			return nil, ErrWalletCreateNotStarted
		}
		notify, err := url.Parse(snapshot.NotifyURL)
		if err != nil {
			return nil, ErrWalletCreateNotStarted
		}
		ret, err := url.Parse(snapshot.ReturnURL)
		if err != nil {
			return nil, ErrWalletCreateNotStarted
		}
		action, fields, err := client.Purchase(&epay.PurchaseArgs{Type: topUp.PaymentMethod, ServiceTradeNo: topUp.TradeNo, Name: "Wallet recharge", Money: snapshot.AmountText, Device: epay.PC, NotifyUrl: notify, ReturnUrl: ret})
		if err != nil {
			return nil, ErrWalletCreateNotStarted
		}
		return &WalletGatewaySession{SessionID: topUp.TradeNo, OrderID: topUp.TradeNo, Checkout: &dto.WalletCheckout{Kind: "form", Method: "POST", Action: action, Fields: fields}}, nil
	case model.PaymentProviderCreem:
		body, err := common.Marshal(map[string]any{"product_id": snapshot.ProductID, "request_id": topUp.TradeNo, "customer": map[string]string{"email": snapshot.Email}, "metadata": map[string]string{"reference_id": topUp.TradeNo}})
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, snapshot.APIURL+"/v1/checkouts", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", snapshot.APIKey)
		client := http.Client{Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		resp, err := client.Do(req)
		if err != nil {
			return nil, ErrWalletProviderUnavailable
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
		if err != nil || len(data) > 64<<10 || resp.StatusCode/100 != 2 {
			return nil, ErrWalletProviderUnavailable
		}
		var remote struct {
			ID  string `json:"id"`
			URL string `json:"checkout_url"`
		}
		if common.Unmarshal(data, &remote) != nil || remote.ID == "" || remote.URL == "" {
			return nil, ErrWalletProviderUnavailable
		}
		return &WalletGatewaySession{SessionID: remote.ID, OrderID: remote.ID, Checkout: &dto.WalletCheckout{Kind: "url", URL: remote.URL}}, nil
	case model.PaymentProviderWaffo:
		env := config.Production
		if snapshot.Environment == "test" {
			env = config.Sandbox
		}
		cfg, err := config.NewConfigBuilder().APIKey(snapshot.APIKey).PrivateKey(snapshot.PrivateKey).WaffoPublicKey(snapshot.PublicKey).Environment(env).MerchantID(snapshot.MerchantID).Build()
		if err != nil {
			return nil, ErrWalletProviderUnavailable
		}
		sdk := waffo.New(cfg)
		resp, err := sdk.Order().Create(ctx, &order.CreateOrderParams{PaymentRequestID: topUp.TradeNo, MerchantOrderID: topUp.TradeNo, OrderAmount: snapshot.AmountText, OrderCurrency: snapshot.Currency, OrderDescription: "Wallet recharge", OrderRequestedAt: time.Unix(topUp.CreateTime, 0).UTC().Format("2006-01-02T15:04:05.000Z"), NotifyURL: snapshot.NotifyURL, MerchantInfo: &order.MerchantInfo{MerchantID: snapshot.MerchantID}, UserInfo: &order.UserInfo{UserID: strconv.Itoa(topUp.UserId), UserEmail: snapshot.Email, UserTerminal: "WEB"}, PaymentInfo: &order.PaymentInfo{ProductName: "ONE_TIME_PAYMENT", PayMethodType: snapshot.PayMethodType, PayMethodName: snapshot.PayMethodName}, GoodsInfo: &order.GoodsInfo{GoodsName: "Wallet recharge", AppName: common.SystemName}, SuccessRedirectURL: snapshot.ReturnURL, FailedRedirectURL: snapshot.ReturnURL}, nil)
		if err != nil || resp == nil || !resp.IsSuccess() {
			return nil, ErrWalletProviderUnavailable
		}
		data := resp.GetData()
		if data == nil || data.PaymentRequestID != topUp.TradeNo || data.MerchantOrderID != topUp.TradeNo || data.FetchRedirectURL() == "" {
			return nil, ErrWalletProviderUnavailable
		}
		return &WalletGatewaySession{SessionID: data.PaymentRequestID, OrderID: data.AcquiringOrderID, Checkout: &dto.WalletCheckout{Kind: "url", URL: data.FetchRedirectURL()}}, nil
	case model.PaymentProviderWaffoPancake:
		expires := 45 * 60
		remote, err := CreateWaffoPancakeCheckoutSession(ctx, &WaffoPancakeCreateSessionParams{MerchantID: snapshot.MerchantID, PrivateKey: snapshot.PrivateKey, ProductID: snapshot.ProductID, BuyerIdentity: snapshot.BuyerIdentity, BuyerEmail: snapshot.Email, OrderMerchantExternalID: topUp.TradeNo, ExpiresInSeconds: &expires, PriceSnapshot: &WaffoPancakePriceSnapshot{Amount: snapshot.AmountText, TaxCategory: "saas"}})
		if err != nil {
			return nil, ErrWalletProviderUnavailable
		}
		deadline, err := time.Parse(time.RFC3339, remote.ExpiresAt)
		if err != nil {
			return nil, ErrWalletProviderUnavailable
		}
		tokenEnd, _ := time.Parse(time.RFC3339, remote.TokenExpiresAt)
		return &WalletGatewaySession{SessionID: remote.SessionID, OrderID: remote.OrderID, ExpiresAt: deadline.Unix(), TokenExpiresAt: tokenEnd.Unix(), Checkout: &dto.WalletCheckout{Kind: "url", URL: remote.CheckoutURL}}, nil
	}
	return nil, ErrWalletProviderUnavailable
}
func ValidWalletCheckout(checkout *dto.WalletCheckout) bool {
	if checkout == nil {
		return false
	}
	target := checkout.URL
	if checkout.Kind == "form" {
		target = checkout.Action
		if checkout.Method != "POST" || len(checkout.Fields) == 0 {
			return false
		}
	} else if checkout.Kind != "url" {
		return false
	}
	u, err := url.Parse(target)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && !strings.ContainsAny(target, "\r\n")
}
