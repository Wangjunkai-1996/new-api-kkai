package controller

import (
	"errors"
	"math"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func shopSettingsDTO(value *model.ShopSettings) (gin.H, error) {
	var retention *model.ShopRetentionPolicy
	var reminders *model.ShopReminderPolicy
	if value.RetentionPolicyJSON != "" && common.UnmarshalJsonStr(value.RetentionPolicyJSON, &retention) != nil {
		return nil, model.ErrShopInvalid
	}
	if value.ReminderPolicyJSON != "" && common.UnmarshalJsonStr(value.ReminderPolicyJSON, &reminders) != nil {
		return nil, model.ErrShopInvalid
	}
	methods := []string{}
	for _, method := range service.ShopConfiguredPaymentMethods() {
		methods = append(methods, method.ID)
	}
	out := gin.H{"version": shopID(value.Version), "new_orders_enabled": value.SalesEnabled, "provider": value.Provider, "provider_account_ref": value.ProviderAccountRef, "environment": value.Environment, "currency": nil, "currency_exponent": value.CurrencyExponent, "payment_methods": methods, "reservation_minutes": value.ReservationSeconds / 60, "pending_order_limit": value.MaxUnpaidOrders, "min_amount_minor": shopID(value.MinAmountMinor), "max_amount_minor": shopID(value.MaxAmountMinor), "retention_policy": retention, "reminder_policy": reminders}
	if value.Currency != "" {
		out["currency"] = value.Currency
	}
	return out, nil
}

func GetShopSettings(c *gin.Context) {
	if err := model.AuthorizeShopAdmin(shopDB(c), shopScope(c)); err != nil {
		shopModelError(c, err)
		return
	}
	value, err := model.GetShopSettings(shopDB(c))
	if err != nil {
		shopModelError(c, err)
		return
	}
	out, err := shopSettingsDTO(value)
	if err != nil {
		shopModelError(c, err)
		return
	}
	shopSuccess(c, 200, out)
}

func UpdateShopSettings(c *gin.Context) {
	var in struct {
		ExpectedVersion    string                    `json:"expected_version,omitempty"`
		NewOrdersEnabled   *bool                     `json:"new_orders_enabled"`
		Provider           string                    `json:"provider"`
		ProviderAccountRef string                    `json:"provider_account_ref"`
		Environment        string                    `json:"environment"`
		Currency           string                    `json:"currency"`
		ReservationMinutes int64                     `json:"reservation_minutes"`
		PendingOrderLimit  int                       `json:"pending_order_limit"`
		MinAmountMinor     string                    `json:"min_amount_minor"`
		MaxAmountMinor     string                    `json:"max_amount_minor"`
		RetentionPolicy    model.ShopRetentionPolicy `json:"retention_policy"`
		ReminderPolicy     model.ShopReminderPolicy  `json:"reminder_policy"`
		Reason             string                    `json:"reason"`
	}
	if !shopBind(c, &in, 16384) {
		return
	}
	if in.NewOrdersEnabled == nil || in.Provider != "epay" || in.Currency != "CNY" || in.Environment != "production" && in.Environment != "test" || in.ReservationMinutes < 1 || in.ReservationMinutes > 1440 {
		shopModelError(c, model.ErrShopInvalid)
		return
	}
	minimum, err := shopVersion(in.MinAmountMinor)
	if err != nil {
		shopModelError(c, err)
		return
	}
	maximum, err := shopVersion(in.MaxAmountMinor)
	if err != nil {
		shopModelError(c, err)
		return
	}
	command, ok := shopCommand(c, in, "settings", "1", in.ExpectedVersion)
	if !ok {
		return
	}
	if command.ExpectedVersion == math.MaxInt64 {
		shopModelError(c, model.ErrShopInvalid)
		return
	}
	retention, err := common.Marshal(in.RetentionPolicy)
	if err != nil {
		shopModelError(c, err)
		return
	}
	reminders, err := common.Marshal(in.ReminderPolicy)
	if err != nil {
		shopModelError(c, err)
		return
	}
	value := model.ShopSettings{SalesEnabled: *in.NewOrdersEnabled, Provider: in.Provider, ProviderAccountRef: in.ProviderAccountRef, Environment: in.Environment, Currency: in.Currency, CurrencyExponent: 2, MinAmountMinor: minimum, MaxAmountMinor: maximum, ReservationSeconds: in.ReservationMinutes * 60, MaxUnpaidOrders: in.PendingOrderLimit, PolicyVersion: command.ExpectedVersion + 1, RetentionPolicyJSON: string(retention), ReminderPolicyJSON: string(reminders)}
	previous, ok := shopCommitted(c, command, "settings", "1")
	if !ok {
		return
	}
	if previous == nil && value.SalesEnabled && !service.ShopPaymentReady(&value) {
		shopModelError(c, service.ErrShopChannelUnavailable)
		return
	}
	updated, err := model.UpdateShopSettings(shopDB(c), command, value, in.Reason)
	if err != nil {
		shopModelError(c, err)
		return
	}
	out, err := shopSettingsDTO(updated)
	if err != nil {
		shopModelError(c, err)
		return
	}
	shopSuccess(c, 200, out)
}

var errShopAmountLimit = errors.New("SHOP_AMOUNT_LIMIT_EXCEEDED")

// Accumulate minor units without database-specific SUM casts, rejecting totals
// above the public money contract instead of overflowing or rounding them.
func shopMoneyTotal(query *gorm.DB, currency string, exponent int) (gin.H, error) {
	rows, err := query.Select("amount_minor").Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var total int64
	for rows.Next() {
		var value int64
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		if value < 0 {
			return nil, model.ErrShopInvalid
		}
		if value > math.MaxInt64-total {
			return nil, errShopAmountLimit
		}
		total += value
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return shopMoney(total, currency, exponent), nil
}

func GetShopOverview(c *gin.Context) {
	db := shopDB(c)
	if err := model.AuthorizeShopAdmin(db, shopScope(c)); err != nil {
		shopModelError(c, err)
		return
	}
	settings, err := model.GetShopSettings(db)
	if err != nil {
		shopModelError(c, err)
		return
	}
	now := time.Now().UTC()
	to, from := now, now.AddDate(0, 0, -30)
	for name, dest := range map[string]*time.Time{"from": &from, "to": &to} {
		if raw := c.Query(name); raw != "" {
			value, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				shopModelError(c, model.ErrShopInvalid)
				return
			}
			*dest = value.UTC()
		}
	}
	if from.Unix() <= 0 || !from.Before(to) || to.After(now.Add(time.Minute)) {
		shopModelError(c, model.ErrShopInvalid)
		return
	}
	currency := c.Query("currency")
	if currency == "" {
		currency = settings.Currency
	}
	if currency != settings.Currency {
		shopModelError(c, model.ErrShopInvalid)
		return
	}
	out := gin.H{"currency": currency, "from": shopTime(from.Unix()), "to": shopTime(to.Unix()), "orders": int64(0), "pending_delivery": int64(0), "pending_aftersales": int64(0), "unresolved_receipts": int64(0), "unresolved_refunds": int64(0), "received": nil, "refunded": nil}
	if currency == "" {
		shopSuccess(c, 200, out)
		return
	}
	orders := db.Model(&model.ShopOrder{}).Where("tenant_id = ? AND currency = ? AND currency_exponent = ?", model.PlatformTenantID, currency, settings.CurrencyExponent)
	orderIDs := orders.Session(&gorm.Session{}).Select("id")
	// Pending responsibilities remain visible regardless of age. The returned
	// interval applies to new orders and monetary movements, not the work queue.
	queries := map[string]*gorm.DB{
		"orders":              orders.Session(&gorm.Session{}).Where("created_at >= ? AND created_at < ?", from.Unix(), to.Unix()),
		"pending_delivery":    orders.Session(&gorm.Session{}).Where("payment_state = ? AND delivery_state <> ?", "paid", "delivered"),
		"pending_aftersales":  db.Model(&model.ShopAftersale{}).Where("tenant_id = ? AND order_id IN (?) AND closed_at = 0", model.PlatformTenantID, orderIDs),
		"unresolved_receipts": db.Model(&model.ShopPaymentReceipt{}).Where("currency = ? AND currency_exponent = ? AND processing_state IN ?", currency, settings.CurrencyExponent, []string{"pending", "review", "extra"}),
		"unresolved_refunds":  db.Model(&model.ShopRefundCase{}).Where("currency = ? AND currency_exponent = ? AND state IN ?", currency, settings.CurrencyExponent, []string{"approved", "processing", "unknown", "failed"}),
	}
	for name, query := range queries {
		var count int64
		if err := query.Count(&count).Error; err != nil {
			shopModelError(c, err)
			return
		}
		out[name] = count
	}
	received, err := shopMoneyTotal(db.Model(&model.ShopPaymentReceipt{}).Where("currency = ? AND currency_exponent = ? AND provider_paid_at >= ? AND provider_paid_at < ?", currency, settings.CurrencyExponent, from.Unix(), to.Unix()), currency, settings.CurrencyExponent)
	if err != nil {
		if errors.Is(err, errShopAmountLimit) {
			shopError(c, 409, errShopAmountLimit.Error())
			return
		}
		shopModelError(c, err)
		return
	}
	// Both manual shop refunds and externally observed refunds converge on one
	// successful adjustment, preventing duplicate totals through attempt joins.
	refunded, err := shopMoneyTotal(db.Model(&model.ShopPaymentAdjustment{}).Where("currency = ? AND currency_exponent = ? AND kind IN ? AND state = ? AND verified_at >= ? AND verified_at < ?", currency, settings.CurrencyExponent, []string{"refund", "external_refund"}, "succeeded", from.Unix(), to.Unix()), currency, settings.CurrencyExponent)
	if err != nil {
		if errors.Is(err, errShopAmountLimit) {
			shopError(c, 409, errShopAmountLimit.Error())
			return
		}
		shopModelError(c, err)
		return
	}
	out["received"], out["refunded"] = received, refunded
	shopSuccess(c, 200, out)
}
