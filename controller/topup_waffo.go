package controller

import (
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
	waffo "github.com/waffo-com/waffo-go"
	"github.com/waffo-com/waffo-go/config"
	"github.com/waffo-com/waffo-go/core"
	"github.com/waffo-com/waffo-go/types/order"
)

func getWaffoSDK() (*waffo.Waffo, error) {
	env := config.Sandbox
	apiKey := setting.WaffoSandboxApiKey
	privateKey := setting.WaffoSandboxPrivateKey
	publicKey := setting.WaffoSandboxPublicCert
	if !setting.WaffoSandbox {
		env = config.Production
		apiKey = setting.WaffoApiKey
		privateKey = setting.WaffoPrivateKey
		publicKey = setting.WaffoPublicCert
	}
	builder := config.NewConfigBuilder().
		APIKey(apiKey).
		PrivateKey(privateKey).
		WaffoPublicKey(publicKey).
		Environment(env)
	if setting.WaffoMerchantId != "" {
		builder = builder.MerchantID(setting.WaffoMerchantId)
	}
	cfg, err := builder.Build()
	if err != nil {
		return nil, err
	}
	return waffo.New(cfg), nil
}

func getWaffoUserEmail(user *model.User) string {
	return fmt.Sprintf("%d@examples.com", user.Id)
}

func getWaffoCurrency() string {
	if setting.WaffoCurrency != "" {
		return setting.WaffoCurrency
	}
	return "USD"
}

func buildWaffoTopUpGoodsInfo(amount int64) *order.GoodsInfo {
	appName := strings.TrimSpace(common.SystemName)
	if appName == "" {
		appName = "New API"
	}
	return &order.GoodsInfo{
		GoodsName: fmt.Sprintf("Recharge %d credits", amount),
		AppName:   appName,
	}
}

// zeroDecimalCurrencies 零小数位币种，金额不能带小数点
var zeroDecimalCurrencies = map[string]bool{
	"IDR": true, "JPY": true, "KRW": true, "VND": true,
}

func formatWaffoAmount(amount float64, currency string) string {
	if zeroDecimalCurrencies[currency] {
		return fmt.Sprintf("%.0f", amount)
	}
	return fmt.Sprintf("%.2f", amount)
}

// getWaffoPayMoney converts the user-facing amount to USD for Waffo payment.
// Waffo only accepts USD, so this function handles the conversion from different
// display types (USD/CNY/TOKENS) to the actual USD amount to charge.
func getWaffoPayMoney(amount float64, group string) float64 {
	originalAmount := amount
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		amount = amount / common.QuotaPerUnit
	}
	topupGroupRatio, discount := topUpPromotionFactors(group, int64(originalAmount))
	return amount * setting.WaffoUnitPrice * topupGroupRatio * discount
}

type WaffoPayRequest struct {
	Amount         int64  `json:"amount"`
	PayMethodIndex *int   `json:"pay_method_index"` // 服务端支付方式列表的索引，nil 表示由 Waffo 自动选择
	PayMethodType  string `json:"pay_method_type"`  // Deprecated: 兼容旧前端，优先使用 pay_method_index
	PayMethodName  string `json:"pay_method_name"`  // Deprecated: 兼容旧前端，优先使用 pay_method_index
}

func RequestWaffoAmount(c *gin.Context) {
	var req WaffoPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}

	waffoMinTopup := int64(setting.WaffoMinTopUp)
	if req.Amount < waffoMinTopup {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", waffoMinTopup)})
		return
	}
	id := c.GetInt("id")
	if rejectInvalidTopUpQuota(c, id, req.Amount) {
		return
	}

	group, err := model.GetUserGroup(id, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}

	payMoney := getWaffoPayMoney(float64(req.Amount), group)
	if payMoney <= 0.01 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "success", "data": strconv.FormatFloat(payMoney, 'f', 2, 64)})
}

// RequestWaffoPay 创建 Waffo 支付订单
func RequestWaffoPay(c *gin.Context) {
	if walletLegacyPurchase(c, model.PaymentProviderWaffo, requestLegacyWaffo) {
		return
	}
	var req WaffoPayRequest
	if !walletBind(c, &req) {
		return
	}
	if req.PayMethodIndex != nil {
		methods := setting.GetWaffoPayMethods()
		index := *req.PayMethodIndex
		if index < 0 || index >= len(methods) {
			walletError(c, 400, "VALIDATION_FAILED", nil)
			return
		}
		req.PayMethodType = methods[index].PayMethodType
		req.PayMethodName = methods[index].PayMethodName
	}
	walletLegacyCreate(c, dto.WalletQuoteRequest{PaymentProvider: model.PaymentProviderWaffo, PaymentMethod: model.PaymentMethodWaffo, Amount: strconv.FormatInt(req.Amount, 10), WaffoPayMethodType: req.PayMethodType, WaffoPayMethodName: req.PayMethodName})
}

// webhookPayloadWithSubInfo 扩展 PAYMENT_NOTIFICATION，包含 SDK 未定义的 subscriptionInfo 字段
type webhookPayloadWithSubInfo struct {
	EventType string `json:"eventType"`
	Result    struct {
		core.PaymentNotificationResult
		SubscriptionInfo *webhookSubscriptionInfo `json:"subscriptionInfo,omitempty"`
	} `json:"result"`
}

type webhookSubscriptionInfo struct {
	Period              string `json:"period,omitempty"`
	MerchantRequest     string `json:"merchantRequest,omitempty"`
	SubscriptionID      string `json:"subscriptionId,omitempty"`
	SubscriptionRequest string `json:"subscriptionRequest,omitempty"`
}

// WaffoWebhook 处理 Waffo 回调通知（支付/退款/订阅）
func WaffoWebhook(c *gin.Context) {

	bodyBytes, err := io.ReadAll(io.LimitReader(c.Request.Body, 1048577))
	if err != nil || len(bodyBytes) > 1048576 {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Waffo webhook 读取请求体失败 path=%q client_ip=%s error=%q", c.Request.URL.Path, c.ClientIP(), "body unavailable"))
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	var untrusted webhookPayloadWithSubInfo
	if common.Unmarshal(bodyBytes, &untrusted) != nil {
		c.AbortWithStatus(400)
		return
	}
	sdk, err := getWaffoSDK()
	if strings.HasPrefix(untrusted.Result.MerchantOrderID, "WT") {
		_, snapshot, e := service.WalletGatewayForOrder(untrusted.Result.MerchantOrderID, model.PaymentProviderWaffo)
		if e != nil && !service.WalletOrderNotFound(e) || e == nil && snapshot == nil {
			c.AbortWithStatus(503)
			return
		}
		if snapshot != nil {
			sdk, err = service.WalletWaffoClient(*snapshot)
		}
	} else if !isWaffoWebhookEnabled() {
		c.AbortWithStatus(403)
		return
	}

	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Waffo webhook SDK 初始化失败 path=%q client_ip=%s error=%q", c.Request.URL.Path, c.ClientIP(), err.Error()))
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	wh := sdk.Webhook()
	bodyStr := string(bodyBytes)
	signature := c.GetHeader("X-SIGNATURE")

	// 验证请求签名
	if !wh.VerifySignature(bodyStr, signature) {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	var event core.WebhookEvent
	if err := common.Unmarshal(bodyBytes, &event); err != nil {
		sendWaffoWebhookResponse(c, wh, false, "invalid payload")
		return
	}

	switch event.EventType {
	case core.EventPayment:
		// 解析为扩展类型，区分普通支付和订阅支付
		var payload webhookPayloadWithSubInfo
		if err := common.Unmarshal(bodyBytes, &payload); err != nil {
			sendWaffoWebhookResponse(c, wh, false, "invalid payment payload")
			return
		}
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("Waffo webhook 验签并解析成功 event_type=%s merchant_order_id=%s order_status=%s client_ip=%s", event.EventType, payload.Result.MerchantOrderID, payload.Result.OrderStatus, c.ClientIP()))
		handleWaffoPayment(c, wh, &payload.Result.PaymentNotificationResult)
	default:
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("Waffo webhook 忽略事件 event_type=%s client_ip=%s", event.EventType, c.ClientIP()))
		sendWaffoWebhookResponse(c, wh, true, "")
	}
}

// handleWaffoPayment 处理支付完成通知
func handleWaffoPayment(c *gin.Context, wh *core.WebhookHandler, result *core.PaymentNotificationResult) {
	if result.OrderStatus != "PAY_SUCCESS" {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("Waffo 订单状态非成功，忽略充值 trade_no=%s order_status=%s client_ip=%s", result.MerchantOrderID, result.OrderStatus, c.ClientIP()))

		sendWaffoWebhookResponse(c, wh, true, "")
		return
	}

	merchantOrderId := result.MerchantOrderID

	LockOrder(merchantOrderId)
	defer UnlockOrder(merchantOrderId)

	order, snapshot, err := service.WalletGatewayForOrder(merchantOrderId, model.PaymentProviderWaffo)
	if service.WalletOrderNotFound(err) {
		merchant, ok := result.MerchantInfo["merchantId"].(string)
		currency := strings.ToUpper(result.UserCurrency)
		exponent, e := walletCurrencyExponent(currency)
		if !ok || merchant == "" || merchant != setting.WaffoMerchantId || e != nil || result.AcquiringOrderID == "" {
			sendWaffoWebhookResponse(c, wh, false, "unmatched payment identity unavailable")
			return
		}
		minor, e := service.WalletPaymentMinor(result.FinalDealAmount, exponent)
		if e != nil {
			sendWaffoWebhookResponse(c, wh, false, "invalid payment amount")
			return
		}
		env := "prod"
		if setting.WaffoSandbox {
			env = "test"
		}
		err := service.SaveUnmatchedWalletPaymentReceipt(c.Request.Context(), model.WalletPaymentReceipt{Provider: model.PaymentProviderWaffo, ProviderAccountRef: "waffo:" + merchant, ProviderEnvironment: env, TransactionID: result.AcquiringOrderID, AmountMinor: minor, Currency: currency, CurrencyExponent: exponent, PaidAt: service.WalletPaymentTimestamp(result.OrderCompletedAt), EvidenceSource: "verified_callback", EvidenceRef: "waffo-payment:" + result.PaymentRequestID})
		if err != nil {
			sendWaffoWebhookResponse(c, wh, false, "payment persistence unavailable")
		} else {
			sendWaffoWebhookResponse(c, wh, true, "")
		}
		return
	}
	if err != nil {
		sendWaffoWebhookResponse(c, wh, false, "payment verification unavailable")
		return
	}
	if snapshot != nil {
		currency := strings.ToUpper(result.UserCurrency)
		exponent, currencyErr := walletCurrencyExponent(currency)
		minor, e := service.WalletPaymentMinor(result.FinalDealAmount, exponent)
		if e != nil || currencyErr != nil || result.AcquiringOrderID == "" {
			sendWaffoWebhookResponse(c, wh, false, "actual payment evidence unavailable")
			return
		}
		requested, requestErr := service.WalletPaymentMinor(result.OrderAmount, order.CurrencyExponent)
		merchant, merchantOK := result.MerchantInfo["merchantId"].(string)
		pricing := merchantOK && merchant == snapshot.MerchantID && result.PaymentRequestID == order.ProviderSessionID && (order.ProviderOrderID == "" || order.ProviderOrderID == result.AcquiringOrderID) && strings.ToUpper(result.OrderCurrency) == order.Currency && currency == order.Currency && requestErr == nil && requested == order.PaidMinor && minor == requested
		_, err := service.FinalizeWalletPaymentReceipt(order, result.AcquiringOrderID, minor, currency, exponent, service.WalletPaymentTimestamp(result.OrderCompletedAt), "verified_callback", "waffo-payment:"+result.PaymentRequestID, pricing, model.TopUpUserPatch{}, "")
		if err != nil && !service.WalletPaymentReceiptHeld(err) {
			sendWaffoWebhookResponse(c, wh, false, "payment persistence unavailable")
			return
		}
		sendWaffoWebhookResponse(c, wh, true, "")
		return
	}
	if err := model.RechargeWaffo(merchantOrderId, c.ClientIP()); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Waffo 充值处理失败 trade_no=%s client_ip=%s error=%q", merchantOrderId, c.ClientIP(), err.Error()))
		sendWaffoWebhookResponse(c, wh, false, err.Error())
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("Waffo 充值成功 trade_no=%s client_ip=%s", merchantOrderId, c.ClientIP()))
	sendWaffoWebhookResponse(c, wh, true, "")
}

// sendWaffoWebhookResponse 发送签名响应
func sendWaffoWebhookResponse(c *gin.Context, wh *core.WebhookHandler, success bool, msg string) {
	var body, sig string
	if success {
		body, sig = wh.BuildSuccessResponse()
	} else {
		body, sig = wh.BuildFailedResponse(msg)
	}
	c.Header("X-SIGNATURE", sig)
	c.Data(http.StatusOK, "application/json", []byte(body))
}
