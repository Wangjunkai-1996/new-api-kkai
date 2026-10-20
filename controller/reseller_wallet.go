package controller

import (
	"errors"
	"math"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

func GetResellerEarnings(c *gin.Context) {
	tenant, _, db, ok := resellerTenant(c)
	if !ok {
		resellerNotReady(c)
		return
	}
	wallet := model.ResellerWallet{TenantID: tenant.TenantID}
	err := db.Where("tenant_id = ?", tenant.TenantID).First(&wallet).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		common.ApiError(c, err)
		return
	}
	var ledger []model.ResellerLedgerEntry
	if err := db.Where("tenant_id = ? AND kind <> ?", tenant.TenantID, model.ResellerLedgerKindUsageReconciliation).Order("id DESC").Limit(200).Find(&ledger).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var quotes []model.PricingQuote
	if err := db.Where("tenant_id = ?", tenant.TenantID).Order("id DESC").Limit(200).Find(&quotes).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var total struct{ Amount int64 }
	if err := db.Model(&model.ResellerLedgerEntry{}).Where("tenant_id = ? AND kind IN ?", tenant.TenantID, []string{model.ResellerLedgerKindSettlement, model.ResellerLedgerKindChargeback}).Select("COALESCE(SUM(amount_minor),0) AS amount").Scan(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var lots []model.CustomerPaymentLot
	if err := db.Where("tenant_id = ? AND remaining_quota > 0 AND refund_status = ?", tenant.TenantID, "none").Find(&lots).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	remaining, remainingFees := decimal.Zero, decimal.Zero
	for _, lot := range lots {
		if lot.CreditedQuota > 0 {
			consumed := decimal.NewFromInt(lot.ConsumedQuota)
			credited := decimal.NewFromInt(lot.CreditedQuota)
			paid := decimal.NewFromInt(lot.PaidMinor)
			fee := decimal.NewFromInt(lot.PaymentFeeMinor)
			// Match the cumulative FIFO allocation, including its unconsumed fen.
			remaining = remaining.Add(paid.Sub(paid.Mul(consumed).Div(credited).Floor()))
			remainingFees = remainingFees.Add(fee.Sub(fee.Mul(consumed).Div(credited).Floor()))
		}
	}
	estimate := decimal.Zero
	pricing := service.NewTenantPricingService(db)
	catalog, err := pricing.ResolveCatalog(c.Request.Context(), tenant.TenantID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	for _, defaultEntry := range catalog {
		if !defaultEntry.IsDefault {
			continue
		}
		price, err := pricing.ResolveCurrentPrice(c.Request.Context(), tenant.TenantID, defaultEntry.Model, defaultEntry.SalesGroup)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		multiplier, err := decimal.NewFromString(price.SalesMultiplier)
		supply := ratio_setting.GetGroupRatio(price.Group)
		if err == nil && multiplier.IsPositive() && !math.IsNaN(supply) && !math.IsInf(supply, 0) && supply >= 0 {
			estimate = remaining.Sub(remainingFees).Sub(remaining.Mul(decimal.NewFromFloat(supply).Div(multiplier)))
		}
		break
	}
	if estimate.GreaterThan(decimal.NewFromInt(math.MaxInt64)) || estimate.LessThan(decimal.NewFromInt(math.MinInt64)) {
		common.ApiError(c, model.ErrResellerWalletInvariant)
		return
	}
	fee, err := strconv.ParseInt(common.GetEnvOrDefaultString("RESELLER_WITHDRAWAL_FEE_MINOR", "0"), 10, 64)
	if err != nil || fee < 0 {
		common.ApiError(c, model.ErrResellerFinanceInvalidInput)
		return
	}
	common.ApiSuccess(c, gin.H{"withdrawal_fee_minor": fee, "wallet": wallet, "ledger": ledger, "quotes": quotes, "estimated_profit_minor": estimate.Floor().IntPart(), "total_profit_minor": total.Amount, "estimate_is_reference": true})
}

func GetResellerWithdrawals(c *gin.Context) {
	tenant, _, db, ok := resellerTenant(c)
	if !ok {
		resellerNotReady(c)
		return
	}
	var withdrawals []model.ResellerWithdrawal
	if err := db.Where("tenant_id = ?", tenant.TenantID).Order("id DESC").Limit(200).Find(&withdrawals).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, withdrawals)
}

func RequestResellerWithdrawal(c *gin.Context) {
	tenant, _, db, ok := resellerTenant(c)
	if !ok {
		resellerNotReady(c)
		return
	}
	var request struct {
		AmountMinor    int64  `json:"amount_minor"`
		IdempotencyKey string `json:"idempotency_key"`
		AlipayAccount  string `json:"alipay_account"`
		AlipayName     string `json:"alipay_name"`
	}
	if common.DecodeJson(c.Request.Body, &request) != nil {
		resellerBadRequest(c, "invalid withdrawal")
		return
	}
	fee, err := strconv.ParseInt(common.GetEnvOrDefaultString("RESELLER_WITHDRAWAL_FEE_MINOR", "0"), 10, 64)
	if err != nil || fee < 0 {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "withdrawal fee is unavailable"})
		return
	}
	withdrawal, err := service.NewResellerFinanceService(db).RequestWithdrawal(c.Request.Context(), model.ResellerWithdrawalInput{TenantID: tenant.TenantID, AmountMinor: request.AmountMinor, IdempotencyKey: request.IdempotencyKey, FeeMinor: fee, AlipayAccount: request.AlipayAccount, AlipayName: request.AlipayName, FeePolicySnapshot: "fixed_cny_fen:" + strconv.FormatInt(fee, 10)})
	if err != nil {
		resellerBadRequest(c, "withdrawal requires at least CNY 100, sufficient available funds and no debt")
		return
	}
	common.ApiSuccess(c, withdrawal)
}
