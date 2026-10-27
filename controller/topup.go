package controller

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

func GetTopUpInfo(c *gin.Context) {
	lifecycleReady, err := walletLifecycleReady(c.Request.Context(), model.DB)
	if err != nil {
		walletError(c, http.StatusServiceUnavailable, "WALLET_STATE_UNAVAILABLE", nil)
		return
	}
	complianceConfirmed := operation_setting.IsPaymentComplianceConfirmed()
	enableCreem := isCreemTopUpEnabled()
	if lifecycleReady {
		enableCreem = isCreemWalletTopUpEnabled()
	}

	// 获取支付方式
	payMethods := operation_setting.PayMethods
	if !complianceConfirmed {
		payMethods = []map[string]string{}
	}

	// 如果启用了 Stripe 支付，添加到支付方法列表
	if isStripeTopUpEnabled() {
		// 检查是否已经包含 Stripe
		hasStripe := false
		for _, method := range payMethods {
			if method["type"] == "stripe" {
				hasStripe = true
				break
			}
		}

		if !hasStripe {
			stripeMethod := map[string]string{
				"name":      "Stripe",
				"type":      "stripe",
				"color":     "rgba(var(--semi-purple-5), 1)",
				"min_topup": strconv.Itoa(setting.StripeMinTopUp),
			}
			payMethods = append(payMethods, stripeMethod)
		}
	}

	// Waffo Pancake displayed above the legacy Waffo gateway.
	enableWaffoPancake := isWaffoPancakeTopUpEnabled()
	if enableWaffoPancake {
		hasWaffoPancake := false
		for _, method := range payMethods {
			if method["type"] == model.PaymentMethodWaffoPancake {
				hasWaffoPancake = true
				break
			}
		}

		if !hasWaffoPancake {
			payMethods = append(payMethods, map[string]string{
				"name":      "Waffo Pancake",
				"type":      model.PaymentMethodWaffoPancake,
				"color":     "rgba(var(--semi-orange-5), 1)",
				"min_topup": strconv.Itoa(setting.WaffoPancakeMinTopUp),
			})
		}
	}

	// 如果启用了 Waffo 支付，添加到支付方法列表
	enableWaffo := isWaffoTopUpEnabled()
	if enableWaffo {
		hasWaffo := false
		for _, method := range payMethods {
			if method["type"] == model.PaymentMethodWaffo {
				hasWaffo = true
				break
			}
		}

		if !hasWaffo {
			waffoMethod := map[string]string{
				"name":      "Waffo (Global Payment)",
				"type":      model.PaymentMethodWaffo,
				"color":     "rgba(var(--semi-blue-5), 1)",
				"min_topup": strconv.Itoa(setting.WaffoMinTopUp),
			}
			payMethods = append(payMethods, waffoMethod)
		}
	}

	data := gin.H{
		"wallet_lifecycle_ready":           lifecycleReady,
		"enable_online_topup":              isEpayTopUpEnabled(),
		"enable_stripe_topup":              isStripeTopUpEnabled(),
		"enable_creem_topup":               enableCreem,
		"enable_waffo_topup":               enableWaffo,
		"enable_waffo_pancake_topup":       enableWaffoPancake,
		"enable_redemption":                complianceConfirmed,
		"payment_compliance_confirmed":     complianceConfirmed,
		"payment_compliance_terms_version": operation_setting.CurrentComplianceTermsVersion,
		"waffo_pay_methods": func() interface{} {
			if enableWaffo {
				return setting.GetWaffoPayMethods()
			}
			return nil
		}(),
		"creem_products":          setting.CreemProducts,
		"pay_methods":             payMethods,
		"min_topup":               operation_setting.MinTopUp,
		"stripe_min_topup":        setting.StripeMinTopUp,
		"waffo_min_topup":         setting.WaffoMinTopUp,
		"waffo_pancake_min_topup": setting.WaffoPancakeMinTopUp,
		"amount_options":          operation_setting.GetPaymentSetting().AmountOptions,
		"discount":                operation_setting.GetPaymentSetting().AmountDiscount,
		"topup_link":              common.TopUpLink,
	}
	if tenant, ok := middleware.GetTenantContext(c); ok && tenant.TenantID > 0 {
		data["enable_stripe_topup"], data["enable_creem_topup"] = false, false
		data["enable_waffo_topup"], data["enable_waffo_pancake_topup"] = false, false
		data["enable_redemption"] = false
		data["creem_products"], data["waffo_pay_methods"] = "[]", nil
		data["discount"], data["topup_discounts_disabled"] = map[int]float64{}, true
		methods := make([]map[string]string, 0)
		for _, method := range payMethods {
			if method["type"] == "alipay" || method["type"] == "wxpay" {
				methods = append(methods, method)
			}
		}
		data["pay_methods"] = methods
	}
	common.ApiSuccess(c, data)
}

type EpayRequest struct {
	Amount        int64  `json:"amount"`
	PaymentMethod string `json:"payment_method"`
}

type AmountRequest struct {
	Amount int64 `json:"amount"`
}

func GetEpayClient() *epay.Client {
	if operation_setting.PayAddress == "" || operation_setting.EpayId == "" || operation_setting.EpayKey == "" {
		return nil
	}
	withUrl, err := epay.NewClient(&epay.Config{
		PartnerID: operation_setting.EpayId,
		Key:       operation_setting.EpayKey,
	}, operation_setting.PayAddress)
	if err != nil {
		return nil
	}
	return withUrl
}

func getPayMoney(amount int64, group string) float64 {
	return getPayMoneyDecimal(amount, group, false).InexactFloat64()
}

func getPayMoneyDecimal(amount int64, group string, tenant bool) decimal.Decimal {
	if !walletPositiveFinite(common.QuotaPerUnit, operation_setting.Price) {
		return decimal.Zero
	}
	dAmount := decimal.NewFromInt(amount)
	// 充值金额以“展示类型”为准：
	// - USD/CNY: 前端传 amount 为金额单位；TOKENS: 前端传 tokens，需要换成 USD 金额
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		dAmount = dAmount.Div(dQuotaPerUnit)
	}

	topupGroupRatio, discount := topUpPromotionFactors(group, amount)
	if tenant {
		topupGroupRatio, discount = 1, 1
	}
	if !walletPositiveFinite(topupGroupRatio, discount) {
		return decimal.Zero
	}
	dTopupGroupRatio := decimal.NewFromFloat(topupGroupRatio)
	dPrice := decimal.NewFromFloat(operation_setting.Price)
	dDiscount := decimal.NewFromFloat(discount)

	payMoney := dAmount.Mul(dPrice).Mul(dTopupGroupRatio).Mul(dDiscount)

	return payMoney
}

func getMinTopup() int64 {
	minTopup := operation_setting.MinTopUp
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		if common.QuotaPerUnit <= 0 || math.IsNaN(common.QuotaPerUnit) || math.IsInf(common.QuotaPerUnit, 0) {
			return math.MaxInt64
		}
		dMinTopup := decimal.NewFromInt(int64(minTopup))
		dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		minimum := dMinTopup.Mul(dQuotaPerUnit).Round(0)
		if minimum.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
			return math.MaxInt64
		}
		return minimum.IntPart()
	}
	return int64(minTopup)
}

func getTopUpQuota(amount int64) (int64, error) {
	if common.QuotaPerUnit <= 0 || math.IsNaN(common.QuotaPerUnit) || math.IsInf(common.QuotaPerUnit, 0) {
		return 0, errors.New("额度单位配置错误")
	}
	quota := decimal.NewFromInt(amount)
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		quotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		storedAmount := quota.Div(quotaPerUnit).Truncate(0)
		if storedAmount.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
			return 0, errors.New("充值额度超出系统可表示范围")
		}
		quota = storedAmount.Mul(quotaPerUnit)
	} else {
		quota = quota.Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	}
	return validateCreditedQuota(quota)
}

func getMaxTopUpAmount() int64 {
	if common.QuotaPerUnit <= 0 || math.IsNaN(common.QuotaPerUnit) || math.IsInf(common.QuotaPerUnit, 0) {
		return 0
	}
	quotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
	maxQuota := decimal.NewFromInt(math.MaxInt64)
	maxStoredAmount := maxQuota.
		Div(quotaPerUnit).
		Floor()
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		if maxStoredAmount.GreaterThan(maxQuota) {
			maxStoredAmount = maxQuota
		}
		maxAmount := maxStoredAmount.Add(decimal.NewFromInt(1)).
			Mul(quotaPerUnit).
			Ceil().
			Sub(decimal.NewFromInt(1))
		if maxAmount.GreaterThan(maxQuota) {
			return math.MaxInt64
		}
		return maxAmount.IntPart()
	}
	if maxStoredAmount.GreaterThan(maxQuota) {
		return math.MaxInt64
	}
	return maxStoredAmount.IntPart()
}

func validateCreditedQuota(quota decimal.Decimal) (int64, error) {
	rounded := quota.Round(0)
	if !rounded.IsPositive() {
		return 0, errors.New("充值额度必须大于 0")
	}
	if rounded.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return 0, errors.New("充值额度超出系统可表示范围")
	}
	return rounded.IntPart(), nil
}

func validateTopUpQuota(amount int64) (int64, error) {
	quota, err := getTopUpQuota(amount)
	if err == nil && quota > 0 {
		return quota, nil
	}
	maxAmount := getMaxTopUpAmount()
	if maxAmount > 0 && amount > maxAmount {
		return 0, fmt.Errorf("单笔充值数量不能大于 %d", maxAmount)
	}
	return 0, errors.New("充值数量无效")
}

func rejectInvalidCreditedQuota(c *gin.Context, userId int, quota decimal.Decimal) bool {
	creditedQuota, err := validateCreditedQuota(quota)
	if err == nil {
		err = model.ValidateTopUpQuotaCapacity(userId, creditedQuota)
	}
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": err.Error()})
		return true
	}
	return false
}

func rejectInvalidTopUpQuota(c *gin.Context, userId int, amount int64) bool {
	creditedQuota, err := validateTopUpQuota(amount)
	if err == nil {
		err = model.ValidateTopUpQuotaCapacity(userId, creditedQuota)
	}
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": err.Error()})
		return true
	}
	return false
}

func RequestEpay(c *gin.Context) {
	if walletLegacyPurchase(c, model.PaymentProviderEpay, requestLegacyEpay) {
		return
	}
	var req EpayRequest
	if !walletBind(c, &req) {
		return
	}
	walletLegacyCreate(c, dto.WalletQuoteRequest{PaymentProvider: model.PaymentProviderEpay, PaymentMethod: req.PaymentMethod, Amount: strconv.FormatInt(req.Amount, 10)})
}

// tradeNo lock
var orderLocks sync.Map
var createLock sync.Mutex

// refCountedMutex 带引用计数的互斥锁，确保最后一个使用者才从 map 中删除
type refCountedMutex struct {
	mu       sync.Mutex
	refCount int
}

// LockOrder 尝试对给定订单号加锁
func LockOrder(tradeNo string) {
	createLock.Lock()
	var rcm *refCountedMutex
	if v, ok := orderLocks.Load(tradeNo); ok {
		rcm = v.(*refCountedMutex)
	} else {
		rcm = &refCountedMutex{}
		orderLocks.Store(tradeNo, rcm)
	}
	rcm.refCount++
	createLock.Unlock()
	rcm.mu.Lock()
}

// UnlockOrder 释放给定订单号的锁
func UnlockOrder(tradeNo string) {
	v, ok := orderLocks.Load(tradeNo)
	if !ok {
		return
	}
	rcm := v.(*refCountedMutex)
	rcm.mu.Unlock()

	createLock.Lock()
	rcm.refCount--
	if rcm.refCount == 0 {
		orderLocks.Delete(tradeNo)
	}
	createLock.Unlock()
}

func EpayNotify(c *gin.Context) {

	var params map[string]string

	if c.Request.Method == "POST" {
		// POST 请求：从 POST body 解析参数
		if err := c.Request.ParseForm(); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 webhook POST 表单解析失败 path=%q client_ip=%s error=%q", c.Request.URL.Path, c.ClientIP(), err.Error()))
			_, _ = c.Writer.Write([]byte("fail"))
			return
		}
		params = lo.Reduce(lo.Keys(c.Request.PostForm), func(r map[string]string, t string, i int) map[string]string {
			r[t] = c.Request.PostForm.Get(t)
			return r
		}, map[string]string{})
	} else {
		// GET 请求：从 URL Query 解析参数
		params = lo.Reduce(lo.Keys(c.Request.URL.Query()), func(r map[string]string, t string, i int) map[string]string {
			r[t] = c.Request.URL.Query().Get(t)
			return r
		}, map[string]string{})
	}

	if len(params) == 0 {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("易支付 webhook 参数为空 path=%q client_ip=%s", c.Request.URL.Path, c.ClientIP()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	if handled, err := service.HandleWalletEpayCallback(c.Request.Context(), params); handled {
		if err != nil {
			_, _ = c.Writer.Write([]byte("fail"))
		} else {
			_, _ = c.Writer.Write([]byte("success"))
		}
		return
	}
	if !isEpayWebhookEnabled() {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	client := GetEpayClient()
	if client == nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 client 未初始化 path=%q client_ip=%s", c.Request.URL.Path, c.ClientIP()))
		_, err := c.Writer.Write([]byte("fail"))
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 webhook 响应写入失败 path=%q client_ip=%s error=%q", c.Request.URL.Path, c.ClientIP(), err.Error()))
		}
		return
	}
	verifyInfo, err := client.Verify(params)
	if err != nil || !verifyInfo.VerifyStatus {
		if _, writeErr := c.Writer.Write([]byte("fail")); writeErr != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 webhook 响应写入失败 path=%q client_ip=%s error=%q", c.Request.URL.Path, c.ClientIP(), writeErr.Error()))
		}
		if err != nil {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("易支付 webhook 验签失败 path=%q client_ip=%s verify_error=%q", c.Request.URL.Path, c.ClientIP(), err.Error()))
		} else {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("易支付 webhook 验签失败 path=%q client_ip=%s verify_status=false", c.Request.URL.Path, c.ClientIP()))
		}
		return
	}

	if verifyInfo.TradeStatus != epay.StatusTradeSuccess {
		_, _ = c.Writer.Write([]byte("success"))
		return
	}

	if _, err := model.FindTopUpByTradeNo(verifyInfo.ServiceTradeNo); service.WalletOrderNotFound(err) && model.WalletPaymentSchemaAvailable() {
		minor, err := model.ParseCNYMinor(params["money"])
		if err != nil || params["pid"] != operation_setting.EpayId || params["trade_no"] == "" {
			_, _ = c.Writer.Write([]byte("fail"))
			return
		}
		err = service.SaveUnmatchedWalletPaymentReceipt(c.Request.Context(), model.WalletPaymentReceipt{Provider: model.PaymentProviderEpay, ProviderAccountRef: "epay:" + walletHash([]string{operation_setting.PayAddress, operation_setting.EpayId}), ProviderEnvironment: "prod", TransactionID: params["trade_no"], AmountMinor: minor, Currency: "CNY", CurrencyExponent: 2, PaidAt: 0, EvidenceSource: "verified_callback", EvidenceRef: "epay-notify:" + params["trade_no"]})
		if err != nil {
			_, _ = c.Writer.Write([]byte("fail"))
		} else {
			_, _ = c.Writer.Write([]byte("success"))
		}
		return
	}
	// The process-local lock reduces duplicate work. FinalizeTopUp's row locks
	// and transaction remain the cross-instance correctness boundary.
	LockOrder(verifyInfo.ServiceTradeNo)
	defer UnlockOrder(verifyInfo.ServiceTradeNo)
	result, err := model.RechargeEpayVerified(verifyInfo.ServiceTradeNo, verifyInfo.Type, params["money"], params["trade_no"])
	if err != nil {
		switch {
		case errors.Is(err, model.ErrTopUpNotFound):
		case errors.Is(err, model.ErrPaymentMethodMismatch):
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("易支付 订单支付网关不匹配 trade_no=%s callback_type=%s client_ip=%s", verifyInfo.ServiceTradeNo, verifyInfo.Type, c.ClientIP()))
		case errors.Is(err, model.ErrTopUpStatusInvalid):
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("易支付 订单状态非法 trade_no=%s callback_type=%s client_ip=%s", verifyInfo.ServiceTradeNo, verifyInfo.Type, c.ClientIP()))
		default:
			logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 完成充值事务失败 trade_no=%s client_ip=%s error=%q", verifyInfo.ServiceTradeNo, c.ClientIP(), err.Error()))
		}
		if _, writeErr := c.Writer.Write([]byte("fail")); writeErr != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 webhook 响应写入失败 trade_no=%s client_ip=%s error=%q", verifyInfo.ServiceTradeNo, c.ClientIP(), writeErr.Error()))
		}
		return
	}
	if !result.AlreadyCompleted {
		model.RecordTopupLog(result.TopUp.UserId, fmt.Sprintf("使用在线充值成功，充值金额: %v，支付金额：%f", logger.LogQuota(result.QuotaDelta), result.TopUp.Money), c.ClientIP(), result.TopUp.PaymentMethod, "epay")
	}
	if _, writeErr := c.Writer.Write([]byte("success")); writeErr != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 webhook 响应写入失败 trade_no=%s client_ip=%s error=%q", verifyInfo.ServiceTradeNo, c.ClientIP(), writeErr.Error()))
	}
}

func RequestAmount(c *gin.Context) {
	var req AmountRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}

	if req.Amount < getMinTopup() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", getMinTopup())})
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
	tenant, _ := middleware.GetTenantContext(c)
	payMoney := getPayMoneyDecimal(req.Amount, group, tenant.TenantID > 0).InexactFloat64()
	if payMoney <= 0.01 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": strconv.FormatFloat(payMoney, 'f', 2, 64)})
}

type topUpHistoryResponse struct {
	*common.PageInfo
	TodayPaymentTotal float64 `json:"today_payment_total"`
	TodayPaymentDate  string  `json:"today_payment_date"`
}

func GetUserTopUps(c *gin.Context) {
	if walletFilteredHistory(c, false) {
		return
	}
	userId := c.GetInt("id")
	pageInfo := common.GetPageQuery(c)
	keyword := c.Query("keyword")

	var (
		topups []*model.TopUp
		total  int64
		err    error
	)
	if keyword != "" {
		topups, total, err = model.SearchUserTopUps(userId, keyword, pageInfo)
	} else {
		topups, total, err = model.GetUserTopUps(userId, pageInfo)
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}

	now := time.Now()
	todayPaymentTotal, err := model.GetTopUpTodayPaymentTotal(&userId, now)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	rows, err := walletLegacyHistory(topups)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetItems(rows)
	common.ApiSuccess(c, topUpHistoryResponse{
		PageInfo:          pageInfo,
		TodayPaymentTotal: todayPaymentTotal,
		TodayPaymentDate:  now.Format("2006-01-02"),
	})
}

// GetAllTopUps 管理员获取全平台充值记录
func GetAllTopUps(c *gin.Context) {
	if walletFilteredHistory(c, true) {
		return
	}
	pageInfo := common.GetPageQuery(c)
	keyword := c.Query("keyword")

	var (
		topups []*model.TopUp
		total  int64
		err    error
	)
	if keyword != "" {
		topups, total, err = model.SearchAllTopUps(keyword, pageInfo)
	} else {
		topups, total, err = model.GetAllTopUps(pageInfo)
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}

	now := time.Now()
	todayPaymentTotal, err := model.GetTopUpTodayPaymentTotal(nil, now)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	rows, err := walletLegacyHistory(topups)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetItems(rows)
	common.ApiSuccess(c, topUpHistoryResponse{
		PageInfo:          pageInfo,
		TodayPaymentTotal: todayPaymentTotal,
		TodayPaymentDate:  now.Format("2006-01-02"),
	})
}

type AdminCompleteTopupRequest struct {
	TradeNo string `json:"trade_no"`
}

// AdminCompleteTopUp adapts the old manual-completion route to audited verification.
func AdminCompleteTopUp(c *gin.Context) {
	var req AdminCompleteTopupRequest
	if !walletBind(c, &req) {
		return
	}
	if req.TradeNo == "" || len(req.TradeNo) > 255 {
		walletError(c, 400, "VALIDATION_FAILED", nil)
		return
	}
	var order model.TopUp
	if err := model.WalletTopUpOnlyQuery(model.DB).Where("trade_no = ?", req.TradeNo).First(&order).Error; err != nil {
		walletModelError(c, model.ErrTopUpNotFound)
		return
	}
	if !walletManualReconciliationAllowed(&order) {
		walletModelError(c, model.ErrWalletAttemptEnded)
		return
	}
	if _, err := model.ScheduleWalletTopUpCheck(model.WalletTopUpScope{TenantID: order.TenantID, UserID: order.UserId}, order.TradeNo); err != nil {
		walletModelError(c, err)
		return
	}
	if _, _, err := service.EnqueueSystemTask(service.WalletTopUpReconcileTaskType, nil); err != nil {
		walletModelError(c, err)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("核实充值（旧补单入口） trade_no=%s", order.TradeNo))
	walletSuccess(c, http.StatusAccepted, gin.H{"trade_no": order.TradeNo, "status": "verification_requested"})
}
