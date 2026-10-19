package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// ResolveTenantRequestCatalog runs on every channel selection, including
// retries. Revoked models and disabled tenants cannot survive a cached route.
func ResolveTenantRequestCatalog(ctx context.Context, tenantID int64, modelName string) (*model.TenantCatalogEntry, error) {
	var tenant model.Tenant
	if err := model.DB.WithContext(ctx).Where("id = ? AND status = ?", tenantID, model.TenantStatusActive).First(&tenant).Error; err != nil {
		return nil, err
	}
	var entry model.TenantCatalogEntry
	if err := model.DB.WithContext(ctx).Where("tenant_id = ? AND model = ? AND status = ?", tenantID, modelName, model.TenantCatalogStatusEnabled).First(&entry).Error; err != nil {
		return nil, err
	}
	var defaults int64
	if err := model.DB.WithContext(ctx).Model(&model.TenantCatalogEntry{}).Where("tenant_id = ? AND sales_group = ? AND is_default = ? AND status = ?", tenantID, entry.SalesGroup, true, model.TenantCatalogStatusEnabled).Count(&defaults).Error; err != nil {
		return nil, err
	}
	if defaults != 1 || entry.SalesGroup == "auto" {
		return nil, model.ErrResellerInvalidCatalog
	}
	return &entry, nil
}

func PrepareTenantPricing(c *gin.Context, info *relaycommon.RelayInfo) error {
	tenantID := common.GetContextKeyInt64(c, constant.ContextKeyTenantID)
	if tenantID <= 0 {
		return nil
	}
	if info.Request == nil {
		return model.ErrResellerInvalidPrice
	}
	requestData, err := common.Marshal(info.Request)
	if err != nil {
		return err
	}
	if err := ValidateTenantTextPayload(requestData, info.OriginModelName); err != nil {
		return err
	}
	if info.TenantPriceVersionID > 0 {
		return nil
	}
	entry, err := ResolveTenantRequestCatalog(c.Request.Context(), tenantID, info.OriginModelName)
	if err != nil {
		return err
	}
	pricing := NewTenantPricingService(model.DB)
	price, err := pricing.ResolveCurrentPrice(c.Request.Context(), tenantID, info.OriginModelName, entry.SalesGroup)
	if err != nil {
		return err
	}
	if err := pricing.ValidateMinimumPrice(price.SalesMultiplier, entry.SalesGroup); err != nil {
		return err
	}
	retail, err := decimal.NewFromString(price.SalesMultiplier)
	if err != nil {
		return err
	}
	supply := ratio_setting.GetGroupRatio(entry.SalesGroup)
	if math.IsNaN(supply) || math.IsInf(supply, 0) || supply < 0 || math.IsNaN(operation_setting.Price) || math.IsInf(operation_setting.Price, 0) || operation_setting.Price <= 0 || common.QuotaPerUnit <= 0 {
		return model.ErrResellerInvalidPrice
	}
	cashUnit, supplierUnit, err := model.ResellerQuotaTariff(decimal.NewFromFloat(operation_setting.Price), decimal.NewFromFloat(common.QuotaPerUnit), decimal.NewFromFloat(supply), retail)
	if err != nil {
		return err
	}
	snapshot, err := common.Marshal(map[string]any{"supply_group": entry.SalesGroup, "supply_ratio": decimal.NewFromFloat(supply).String(), "retail_ratio": retail.String(), "cny_per_unit": decimal.NewFromFloat(operation_setting.Price).String(), "quota_per_unit": common.QuotaPerUnit, "rounding": "paid_lot_cumulative_fen", "payment_fee_rate": model.ResellerPaymentFeeRate})
	if err != nil {
		return err
	}
	info.TenantID, info.TenantPriceVersionID = tenantID, price.ID
	info.TenantRetailMultiplier, _ = retail.Float64()
	info.TenantSalesMultiplier = retail.String()
	info.TenantSaleMinorPerQuota = cashUnit
	info.TenantSupplierMinorPerQuota = supplierUnit
	info.TenantSupplySnapshot = string(snapshot)
	info.TenantSupplierPriceMinor = price.SupplierPriceMinor
	info.UsingGroup, info.TokenGroup = entry.SalesGroup, entry.SalesGroup
	return nil
}

type tenantBillingSession struct {
	mu        sync.Mutex
	info      *relaycommon.RelayInfo
	requestID string
	reserved  int
	finalized bool
}

func preConsumeTenantBilling(c *gin.Context, info *relaycommon.RelayInfo, quota int) *types.NewAPIError {
	if info.Billing != nil {
		return nil
	}
	quota = max(quota, 1)
	retail, err := decimal.NewFromString(info.TenantSalesMultiplier)
	if err != nil || !retail.IsPositive() {
		return types.NewError(model.ErrResellerInvalidPrice, types.ErrorCodeModelPriceError, types.ErrOptionWithSkipRetry())
	}
	snapshot, err := common.Marshal(map[string]any{"tariff": info.TenantSupplySnapshot, "price": info.PriceData, "tiered": info.TieredBillingSnapshot})
	if err != nil {
		return types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithSkipRetry())
	}
	sale, err := model.ResellerQuotedCharge(info.TenantSaleMinorPerQuota, int64(quota))
	if err != nil || sale <= 0 {
		return types.NewError(model.ErrResellerInvalidPrice, types.ErrorCodeModelPriceError, types.ErrOptionWithSkipRetry())
	}
	supply, err := model.ResellerQuotedCharge(info.TenantSupplierMinorPerQuota, int64(quota))
	if err != nil {
		return types.NewError(model.ErrResellerInvalidPrice, types.ErrorCodeModelPriceError, types.ErrOptionWithSkipRetry())
	}
	record, err := model.ReserveTenantConsumption(context.WithoutCancel(c.Request.Context()), model.DB, model.ResellerPricingQuoteInput{TenantID: info.TenantID, CustomerID: info.UserId, RequestID: info.RequestId, PriceVersionID: info.TenantPriceVersionID, Model: info.OriginModelName, Group: info.UsingGroup, SaleMinor: sale, SaleMinorPerQuota: info.TenantSaleMinorPerQuota, SupplierPriceMinor: supply, SupplierMinorPerQuota: info.TenantSupplierMinorPerQuota, SupplySnapshot: string(snapshot), SalesMultiplier: retail, QuotedQuota: int64(quota)}, info.TokenId)
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry())
	}
	info.TenantQuoteID = record.QuoteID
	info.FinalPreConsumedQuota = quota
	info.BillingSource = BillingSourceWallet
	info.Billing = &tenantBillingSession{info: info, requestID: record.RequestID, reserved: quota}
	return nil
}

func (s *tenantBillingSession) Settle(actualQuota int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := model.RecordTenantConsumptionResult(model.DB, s.requestID, int64(actualQuota), s.info.ChannelId, false); err != nil {
		return err
	}
	s.finalized = true // A durable usage intent must never be refunded on a later transport error.
	return model.SettleTenantConsumption(context.Background(), model.DB, s.requestID)
}
func (s *tenantBillingSession) Refund(c *gin.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.info.BillingOutcomeUnknown {
		if err := model.MarkTenantConsumptionUncertain(model.DB, s.requestID, s.info.ChannelId); err != nil {
			logger.LogError(c, "tenant reconciliation intent failed: "+err.Error())
		}
		return
	}
	if s.finalized {
		return
	}
	if err := model.RecordTenantConsumptionResult(model.DB, s.requestID, 0, s.info.ChannelId, true); err != nil {
		logger.LogError(c, "tenant refund intent failed: "+err.Error())
		return
	}
	s.finalized = true
	if err := model.SettleTenantConsumption(context.Background(), model.DB, s.requestID); err != nil {
		logger.LogError(c, "tenant refund pending: "+err.Error())
	}
}
func (s *tenantBillingSession) NeedsRefund() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.finalized
}
func (s *tenantBillingSession) GetPreConsumedQuota() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reserved
}
func (s *tenantBillingSession) Reserve(targetQuota int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if targetQuota <= s.reserved {
		return nil
	}
	if s.finalized {
		return model.ErrTenantConsumptionConflict
	}
	if err := model.IncreaseTenantConsumptionReservation(model.DB, s.requestID, int64(targetQuota)); err != nil {
		return err
	}
	s.reserved = targetQuota
	s.info.FinalPreConsumedQuota = targetQuota
	return nil
}

func RegisterTenantConsumptionBackgroundJob(registry *BackgroundJobRegistry) error {
	return registry.Register(BackgroundJob{Name: "tenant-consumption-settle", Interval: time.Minute, RunOnStart: true, WritesData: true, RequiresLeaderLease: true, Run: RunTenantConsumptionSettlements})
}
func RunTenantConsumptionSettlements(ctx context.Context) error {
	var pending []model.TenantConsumption
	if err := model.DB.WithContext(ctx).Where("status IN ?", []string{"settlement_pending", "refund_pending"}).Order("updated_at, id").Limit(100).Find(&pending).Error; err != nil {
		return err
	}
	var failures []error
	for _, row := range pending {
		if err := model.SettleTenantConsumption(ctx, model.DB, row.RequestID); err != nil {
			failures = append(failures, fmt.Errorf("tenant settlement %s: %w", row.RequestID, err))
		}
	}
	return errors.Join(failures...)
}
