package controller

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
)

const CreemSignatureHeader = "creem-signature"

var creemAdaptor = &CreemAdaptor{}

// 生成HMAC-SHA256签名
func generateCreemSignature(payload string, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(payload))
	return hex.EncodeToString(h.Sum(nil))
}

// 验证Creem webhook签名
func verifyCreemSignature(payload string, signature string, secret string) bool {
	if strings.TrimSpace(secret) == "" {
		return false
	}

	expectedSignature := generateCreemSignature(payload, secret)
	return hmac.Equal([]byte(signature), []byte(expectedSignature))
}

type CreemPayRequest struct {
	ProductId     string `json:"product_id"`
	PaymentMethod string `json:"payment_method"`
}

type CreemProduct struct {
	ProductId string  `json:"productId"`
	Name      string  `json:"name"`
	Price     float64 `json:"price"`
	Currency  string  `json:"currency"`
	Quota     int64   `json:"quota"`
}

type CreemAdaptor struct {
}

func (*CreemAdaptor) RequestPay(c *gin.Context, req *CreemPayRequest) {
	walletLegacyCreate(c, dto.WalletQuoteRequest{PaymentProvider: model.PaymentProviderCreem, PaymentMethod: req.PaymentMethod, ProductID: req.ProductId})
}

func RequestCreemPay(c *gin.Context) {
	var req CreemPayRequest
	if !walletBind(c, &req) {
		return
	}
	creemAdaptor.RequestPay(c, &req)
}

// 新的Creem Webhook结构体，匹配实际的webhook数据格式
type CreemWebhookEvent struct {
	Id        string `json:"id"`
	EventType string `json:"eventType"`
	CreatedAt int64  `json:"created_at"`
	Object    struct {
		Id        string `json:"id"`
		Object    string `json:"object"`
		RequestId string `json:"request_id"`
		Order     struct {
			Object      string `json:"object"`
			Id          string `json:"id"`
			Customer    string `json:"customer"`
			Product     string `json:"product"`
			Amount      int    `json:"amount"`
			Currency    string `json:"currency"`
			SubTotal    int    `json:"sub_total"`
			TaxAmount   int    `json:"tax_amount"`
			AmountDue   int    `json:"amount_due"`
			AmountPaid  int    `json:"amount_paid"`
			Status      string `json:"status"`
			Type        string `json:"type"`
			Transaction string `json:"transaction"`
			CreatedAt   string `json:"created_at"`
			UpdatedAt   string `json:"updated_at"`
			Mode        string `json:"mode"`
		} `json:"order"`
		Product struct {
			Id                string  `json:"id"`
			Object            string  `json:"object"`
			Name              string  `json:"name"`
			Description       string  `json:"description"`
			Price             int     `json:"price"`
			Currency          string  `json:"currency"`
			BillingType       string  `json:"billing_type"`
			BillingPeriod     string  `json:"billing_period"`
			Status            string  `json:"status"`
			TaxMode           string  `json:"tax_mode"`
			TaxCategory       string  `json:"tax_category"`
			DefaultSuccessUrl *string `json:"default_success_url"`
			CreatedAt         string  `json:"created_at"`
			UpdatedAt         string  `json:"updated_at"`
			Mode              string  `json:"mode"`
		} `json:"product"`
		Units    int `json:"units"`
		Customer struct {
			Id        string `json:"id"`
			Object    string `json:"object"`
			Email     string `json:"email"`
			Name      string `json:"name"`
			Country   string `json:"country"`
			CreatedAt string `json:"created_at"`
			UpdatedAt string `json:"updated_at"`
			Mode      string `json:"mode"`
		} `json:"customer"`
		Status   string            `json:"status"`
		Metadata map[string]string `json:"metadata"`
		Mode     string            `json:"mode"`
	} `json:"object"`
}

func CreemWebhook(c *gin.Context) {

	// 读取body内容用于打印，同时保留原始数据供后续使用
	bodyBytes, err := io.ReadAll(io.LimitReader(c.Request.Body, 1048577))
	if err != nil || len(bodyBytes) > 1048576 {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Creem webhook 读取请求体失败 path=%q client_ip=%s error=%q", c.Request.URL.Path, c.ClientIP(), "body unavailable"))
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	// 获取签名头
	signature := c.GetHeader(CreemSignatureHeader)
	if signature == "" {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	var untrusted CreemWebhookEvent
	if common.Unmarshal(bodyBytes, &untrusted) != nil {
		c.AbortWithStatus(400)
		return
	}
	unmatched := false
	secret := setting.CreemWebhookSecret
	if strings.HasPrefix(untrusted.Object.RequestId, "WT") {
		original, err := service.WalletPaymentWebhookSecret(untrusted.Object.RequestId, model.PaymentProviderCreem)
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
	// 验证签名
	if !verifyCreemSignature(string(bodyBytes), signature, secret) {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	if unmatched {
		if untrusted.EventType != "checkout.completed" || untrusted.Object.Order.Status != "paid" {
			c.Status(200)
			return
		}
		account := os.Getenv("WALLET_CREEM_ACCOUNT_REF")
		env := "prod"
		if setting.CreemTestMode {
			env = "test"
		}
		transaction := untrusted.Object.Order.Transaction
		if transaction == "" {
			transaction = untrusted.Object.Order.Id
		}
		currency := strings.ToUpper(untrusted.Object.Order.Currency)
		exponent, err := walletCurrencyExponent(currency)
		if account == "" || untrusted.Object.Order.Mode != env || transaction == "" || err != nil {
			c.AbortWithStatus(503)
			return
		}
		if err := service.SaveUnmatchedWalletPaymentReceipt(c.Request.Context(), model.WalletPaymentReceipt{Provider: model.PaymentProviderCreem, ProviderAccountRef: account, ProviderEnvironment: env, TransactionID: transaction, AmountMinor: int64(untrusted.Object.Order.AmountPaid), Currency: currency, CurrencyExponent: exponent, PaidAt: 0, EvidenceSource: "verified_callback", EvidenceRef: "creem-event:" + untrusted.Id}); err != nil {
			c.AbortWithStatus(500)
			return
		}
		c.Status(200)
		return
	}
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("Creem webhook 验签成功 path=%q client_ip=%s", c.Request.URL.Path, c.ClientIP()))

	// 重新设置body供后续的ShouldBindJSON使用
	c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// 解析新格式的webhook数据
	var webhookEvent CreemWebhookEvent
	if err := c.ShouldBindJSON(&webhookEvent); err != nil {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("Creem webhook 解析成功 event_type=%s event_id=%s request_id=%s order_id=%s order_status=%s", webhookEvent.EventType, webhookEvent.Id, webhookEvent.Object.RequestId, webhookEvent.Object.Order.Id, webhookEvent.Object.Order.Status))

	// 根据事件类型处理不同的webhook
	switch webhookEvent.EventType {
	case "checkout.completed":
		handleCheckoutCompleted(c, &webhookEvent)
	default:
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("Creem webhook 忽略事件 event_type=%s event_id=%s", webhookEvent.EventType, webhookEvent.Id))
		c.Status(http.StatusOK)
	}
}

// 处理支付完成事件
func handleCheckoutCompleted(c *gin.Context, event *CreemWebhookEvent) {
	// 验证订单状态
	if event.Object.Order.Status != "paid" {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("Creem 订单状态未支付，忽略处理 request_id=%s order_id=%s order_status=%s", event.Object.RequestId, event.Object.Order.Id, event.Object.Order.Status))
		c.Status(http.StatusOK)
		return
	}

	// 获取引用ID（这是我们创建订单时传递的request_id）
	referenceId := event.Object.RequestId
	if referenceId == "" {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("Creem webhook 缺少 request_id event_id=%s order_id=%s", event.Id, event.Object.Order.Id))
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	if strings.HasPrefix(referenceId, "MEMBER-") {
		if err := completeMembershipCreem(event); err != nil {
			logger.LogError(c.Request.Context(), "membership Creem callback failed: "+err.Error())
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusOK)
		return
	}
	// Try complete subscription order first
	LockOrder(referenceId)
	defer UnlockOrder(referenceId)
	if err := model.CompleteSubscriptionOrder(referenceId, common.GetJsonString(event), model.PaymentProviderCreem, ""); err == nil {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("Creem 订阅订单处理成功 trade_no=%s creem_order_id=%s", referenceId, event.Object.Order.Id))
		c.Status(http.StatusOK)
		return
	} else if err != nil && !errors.Is(err, model.ErrSubscriptionOrderNotFound) {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Creem 订阅订单处理失败 trade_no=%s creem_order_id=%s error=%q", referenceId, event.Object.Order.Id, err.Error()))
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	// 验证订单类型，目前只处理一次性付款（充值）
	if event.Object.Order.Type != "onetime" && !strings.HasPrefix(referenceId, "WT") {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("Creem 暂不支持该订单类型，忽略处理 request_id=%s creem_order_id=%s order_type=%s", referenceId, event.Object.Order.Id, event.Object.Order.Type))
		c.Status(http.StatusOK)
		return
	}

	// 查询本地订单确认存在
	topUp := model.GetTopUpByTradeNo(referenceId)
	if topUp == nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("Creem 充值订单不存在 trade_no=%s creem_order_id=%s", referenceId, event.Object.Order.Id))
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	if topUp.WalletLifecycleVersion == 1 {
		_, snapshot, err := service.WalletGatewayForOrder(referenceId, model.PaymentProviderCreem)
		if err != nil || snapshot == nil {
			c.AbortWithStatus(503)
			return
		}
		transaction := event.Object.Order.Transaction
		if transaction == "" {
			transaction = event.Object.Order.Id
		}
		currency := strings.ToUpper(event.Object.Order.Currency)
		exponent, currencyErr := walletCurrencyExponent(currency)
		if transaction == "" || event.Object.Order.Id == "" || currencyErr != nil || event.Object.Order.AmountPaid < 0 {
			c.AbortWithStatus(400)
			return
		}
		// Creem's checkout event does not carry an authoritative paid_at.
		// Persist it for operations instead of inventing a payment timestamp.
		pricing := event.Object.Order.Type == "onetime" && (topUp.ProviderSessionID == "" || event.Object.Id == topUp.ProviderSessionID) && event.Object.Order.Product == snapshot.ProductID && event.Object.Order.Mode == topUp.ProviderEnvironment && currency == topUp.Currency && event.Object.Units == 1 && int64(event.Object.Order.SubTotal) == topUp.PaidMinor && event.Object.Order.AmountPaid == event.Object.Order.AmountDue && event.Object.Order.TaxAmount >= 0 && event.Object.Order.SubTotal >= 0 && int64(event.Object.Order.SubTotal) <= math.MaxInt64-int64(event.Object.Order.TaxAmount) && int64(event.Object.Order.AmountDue) == int64(event.Object.Order.SubTotal)+int64(event.Object.Order.TaxAmount)
		_, err = service.FinalizeWalletPaymentReceipt(topUp, transaction, int64(event.Object.Order.AmountPaid), currency, exponent, 0, "verified_callback", "creem-event:"+event.Id, pricing, model.TopUpUserPatch{}, "")
		if err != nil && !service.WalletPaymentReceiptHeld(err) {
			c.AbortWithStatus(500)
			return
		}
		c.Status(200)
		return
	}

	// 处理充值，传入客户邮箱和姓名信息
	customerEmail := event.Object.Customer.Email
	customerName := event.Object.Customer.Name

	// 防护性检查，确保邮箱和姓名不为空字符串
	if customerEmail == "" {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("Creem 回调客户邮箱为空 trade_no=%s creem_order_id=%s", referenceId, event.Object.Order.Id))
	}
	if customerName == "" {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("Creem 回调客户姓名为空 trade_no=%s creem_order_id=%s", referenceId, event.Object.Order.Id))
	}
	err := model.RechargeCreem(referenceId, customerEmail, customerName, c.ClientIP())
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Creem 充值处理失败 trade_no=%s creem_order_id=%s client_ip=%s error=%q", referenceId, event.Object.Order.Id, c.ClientIP(), err.Error()))
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("Creem 充值成功 trade_no=%s creem_order_id=%s quota=%d money=%.2f client_ip=%s", referenceId, event.Object.Order.Id, topUp.Amount, topUp.Money, c.ClientIP()))
	c.Status(http.StatusOK)
}

type CreemCheckoutRequest struct {
	ProductId string `json:"product_id"`
	RequestId string `json:"request_id"`
	Customer  struct {
		Email string `json:"email"`
	} `json:"customer"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type CreemCheckoutResponse struct {
	CheckoutUrl string `json:"checkout_url"`
	Id          string `json:"id"`
}

func genCreemLink(ctx context.Context, referenceId string, product *CreemProduct, email string, username string) (string, error) {
	if setting.CreemApiKey == "" {
		return "", fmt.Errorf("未配置Creem API密钥")
	}

	// 根据测试模式选择 API 端点
	apiUrl := "https://api.creem.io/v1/checkouts"
	if setting.CreemTestMode {
		apiUrl = "https://test-api.creem.io/v1/checkouts"
		logger.LogInfo(ctx, fmt.Sprintf("Creem 使用测试环境 api_url=%s", apiUrl))
	}

	// 构建请求数据，确保包含用户邮箱
	requestData := CreemCheckoutRequest{
		ProductId: product.ProductId,
		RequestId: referenceId, // 这个作为订单ID传递给Creem
		Customer: struct {
			Email string `json:"email"`
		}{
			Email: email, // 用户邮箱会在支付页面预填充
		},
		Metadata: map[string]string{
			"username":     username,
			"reference_id": referenceId,
			"product_name": product.Name,
			"quota":        fmt.Sprintf("%d", product.Quota),
		},
	}

	// 序列化请求数据
	jsonData, err := common.Marshal(requestData)
	if err != nil {
		return "", fmt.Errorf("序列化请求数据失败: %v", err)
	}

	// 创建 HTTP 请求
	req, err := http.NewRequest("POST", apiUrl, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("创建HTTP请求失败: %v", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", setting.CreemApiKey)

	logger.LogInfo(ctx, fmt.Sprintf("Creem 支付请求已发送 api_url=%s product_id=%s trade_no=%s", apiUrl, product.ProductId, referenceId))

	// 发送请求
	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("发送HTTP请求失败: %v", err)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %v", err)
	}

	// 检查响应状态
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("Creem API http status %d ", resp.StatusCode)
	}
	// 解析响应
	var checkoutResp CreemCheckoutResponse
	err = common.Unmarshal(body, &checkoutResp)
	if err != nil {
		return "", fmt.Errorf("解析响应失败: %v", err)
	}

	if checkoutResp.CheckoutUrl == "" {
		return "", fmt.Errorf("Creem API resp no checkout url ")
	}

	return checkoutResp.CheckoutUrl, nil
}
