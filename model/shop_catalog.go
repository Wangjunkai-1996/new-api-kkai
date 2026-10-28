package model

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type ShopField struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Required bool   `json:"required"`
}
type ShopWeeklyWindow struct {
	Weekday int    `json:"weekday"`
	Start   string `json:"start"`
	End     string `json:"end"`
}
type ShopSLA struct {
	Clock               string             `json:"clock"`
	DurationMinutes     int64              `json:"duration_minutes"`
	Timezone            string             `json:"timezone"`
	WeeklyWindows       []ShopWeeklyWindow `json:"weekly_windows"`
	PauseOnMissingInput bool               `json:"pause_on_missing_input"`
}
type ShopDeliveryPolicy struct {
	Mode        string   `json:"mode"`
	PublicTerms string   `json:"public_terms"`
	SLA         *ShopSLA `json:"sla"`
}
type ShopViewPolicy struct {
	DurationSeconds  int64  `json:"duration_seconds"`
	FirstStart       string `json:"first_start"`
	ReplacementStart string `json:"replacement_start"`
	ExpiredAction    string `json:"expired_action"`
	PublicTerms      string `json:"public_terms"`
}
type ShopPeriodPolicy struct {
	Mode            string  `json:"mode"`
	StartEvent      string  `json:"start_event"`
	DurationSeconds int64   `json:"duration_seconds"`
	FixedEndAt      *string `json:"fixed_end_at"`
	PublicTerms     string  `json:"public_terms"`
}
type ShopRefundPolicy struct {
	GoodsRefundMode string `json:"goods_refund_mode"`
	Execution       string `json:"execution"`
	PublicTerms     string `json:"public_terms"`
}
type ShopProductTerms struct {
	TemplateVersion   int64              `json:"template_version"`
	DeliveryFields    []ShopField        `json:"delivery_fields"`
	BuyerInputSchema  []ShopField        `json:"buyer_input_schema"`
	UsageConditions   string             `json:"usage_conditions"`
	DeliveryPolicy    ShopDeliveryPolicy `json:"delivery_policy"`
	ViewPolicy        ShopViewPolicy     `json:"view_policy"`
	WarrantyPolicy    ShopPeriodPolicy   `json:"warranty_policy"`
	EntitlementPolicy ShopPeriodPolicy   `json:"entitlement_policy"`
	RefundPolicy      ShopRefundPolicy   `json:"refund_policy"`
}
type ShopRetentionPolicy struct {
	SecretAfterDeliverySeconds     int64 `json:"secret_after_delivery_seconds"`
	BuyerInputAfterClosureSeconds  int64 `json:"buyer_input_after_closure_seconds"`
	EvidenceAfterSettlementSeconds int64 `json:"evidence_after_settlement_seconds"`
	AuditAfterSettlementSeconds    int64 `json:"audit_after_settlement_seconds"`
	IdempotencyAfterClosureSeconds int64 `json:"idempotency_after_closure_seconds"`
}
type ShopReminderPolicy struct {
	PaymentUnknownSeconds     int64 `json:"payment_unknown_seconds"`
	DeliveryExceptionSeconds  int64 `json:"delivery_exception_seconds"`
	RefundVerificationSeconds int64 `json:"refund_verification_seconds"`
}
type ShopOrderSnapshot struct {
	ProductPublicID string              `json:"product_id"`
	Name            string              `json:"name"`
	CoverRef        string              `json:"cover"`
	Specification   string              `json:"specification"`
	ProductKind     string              `json:"product_type"`
	DeliveryType    string              `json:"delivery_type"`
	Terms           ShopProductTerms    `json:"terms"`
	Retention       ShopRetentionPolicy `json:"retention"`
}

func shopTimeAdd(start, seconds int64) (int64, error) {
	if start <= 0 || seconds <= 0 || seconds > math.MaxInt64-start || start+seconds > 253402300799 {
		return 0, ErrShopInvalid
	}
	return start + seconds, nil
}
func shopWindowMinutes(value string) (int, error) {
	t, err := time.Parse("15:04", value)
	if err != nil {
		return 0, ErrShopInvalid
	}
	return t.Hour()*60 + t.Minute(), nil
}
func ValidateShopSLA(s *ShopSLA) error {
	if s == nil || s.DurationMinutes <= 0 || s.DurationMinutes > 525600 || !slices.Contains([]string{"elapsed", "business"}, s.Clock) {
		return ErrShopInvalid
	}
	if _, err := time.LoadLocation(s.Timezone); err != nil {
		return ErrShopInvalid
	}
	if s.Clock == "elapsed" {
		if len(s.WeeklyWindows) != 0 {
			return ErrShopInvalid
		}
		return nil
	}
	if len(s.WeeklyWindows) == 0 || len(s.WeeklyWindows) > 14 {
		return ErrShopInvalid
	}
	for i, w := range s.WeeklyWindows {
		a, e := shopWindowMinutes(w.Start)
		b, f := shopWindowMinutes(w.End)
		if e != nil || f != nil || w.Weekday < 1 || w.Weekday > 7 || a >= b {
			return ErrShopInvalid
		}
		for _, v := range s.WeeklyWindows[:i] {
			x, _ := shopWindowMinutes(v.Start)
			y, _ := shopWindowMinutes(v.End)
			if w.Weekday == v.Weekday && a < y && x < b {
				return ErrShopInvalid
			}
		}
	}
	return nil
}

// Iterate real UTC minutes so DST gaps vanish and repeated local minutes count
// twice. The fixed ten-year horizon bounds sparse calendar input.
func ShopSLADeadline(start int64, s *ShopSLA) (int64, error) {
	if err := ValidateShopSLA(s); err != nil {
		return 0, err
	}
	if s.Clock == "elapsed" {
		return shopTimeAdd(start, s.DurationMinutes*60)
	}
	loc, _ := time.LoadLocation(s.Timezone)
	remaining := s.DurationMinutes * 60
	end, err := shopTimeAdd(start, 3660*86400)
	if err != nil {
		return 0, err
	}
	for at := start; at < end; {
		t := time.Unix(at, 0).In(loc)
		step := int64(60 - t.Second())
		if shopWorkingMinute(t, s) {
			if remaining <= step {
				return at + remaining, nil
			}
			remaining -= step
		}
		at += step
	}
	return 0, ErrShopInvalid
}
func shopWorkingMinute(t time.Time, s *ShopSLA) bool {
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	minute := t.Hour()*60 + t.Minute()
	for _, w := range s.WeeklyWindows {
		a, _ := shopWindowMinutes(w.Start)
		b, _ := shopWindowMinutes(w.End)
		if w.Weekday == weekday && minute >= a && minute < b {
			return true
		}
	}
	return false
}
func shopSLAPauseSeconds(start, end int64, s *ShopSLA) (int64, error) {
	if end < start || end-start > 3660*86400 {
		return 0, ErrShopInvalid
	}
	if s.Clock == "elapsed" {
		return end - start, nil
	}
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return 0, ErrShopInvalid
	}
	var n int64
	for at := start; at < end; {
		t := time.Unix(at, 0).In(loc)
		step := min(int64(60-t.Second()), end-at)
		if shopWorkingMinute(t, s) {
			n += step
		}
		at += step
	}
	return n, nil
}
func shopValidatePeriod(p ShopPeriodPolicy, warranty bool) error {
	if !shopRequired(p.PublicTerms, 8000) {
		return ErrShopInvalid
	}
	switch p.Mode {
	case "none":
		if !warranty {
			return ErrShopInvalid
		}
	case "described":
		if warranty {
			return ErrShopInvalid
		}
	case "duration":
		if !slices.Contains([]string{"paid_at", "first_delivered_at"}, p.StartEvent) {
			return ErrShopInvalid
		}
		if _, err := shopTimeAdd(1, p.DurationSeconds); err != nil {
			return err
		}
	case "fixed_end":
		if p.FixedEndAt == nil {
			return ErrShopInvalid
		}
		t, err := time.Parse(time.RFC3339, *p.FixedEndAt)
		if err != nil || t.Unix() <= 0 {
			return ErrShopInvalid
		}
	default:
		return ErrShopInvalid
	}
	return nil
}
func ValidateShopProduct(p *ShopProduct, publish bool) error {
	if p == nil || !shopRequired(p.Name, 120) || !shopBoundedText(p.Summary, 300) || !shopBoundedText(p.Specification, 8000) || p.PriceMinor < 0 || p.ManualCapacity < 0 || p.LowStockThreshold < 0 || !slices.Contains([]string{"automatic", "manual"}, p.DeliveryType) || !shopBoundedText(p.ProductKind, 32) {
		return ErrShopInvalid
	}
	if p.CoverRef != "" {
		u, e := url.Parse(p.CoverRef)
		if e != nil || u.IsAbs() || u.Host != "" || !strings.HasPrefix(p.CoverRef, "/") || strings.HasPrefix(p.CoverRef, "//") || strings.Contains(p.CoverRef, "\\") {
			return ErrShopInvalid
		}
	}
	if !publish {
		return nil
	}
	if p.PriceMinor <= 0 || len(p.Currency) != 3 || p.Currency != strings.ToUpper(p.Currency) || p.CurrencyExponent < 0 || p.CurrencyExponent > 6 || p.TermsVersion <= 0 {
		return ErrShopInvalid
	}
	var t ShopProductTerms
	if len(p.TermsJSON) > 65536 || common.UnmarshalJsonStr(p.TermsJSON, &t) != nil {
		return ErrShopInvalid
	}
	if t.TemplateVersion <= 0 || len(t.DeliveryFields) == 0 || len(t.DeliveryFields) > 12 || len(t.BuyerInputSchema) > 3 || !shopRequired(t.UsageConditions, 8000) {
		return ErrShopInvalid
	}
	seen := map[string]bool{}
	for _, f := range t.DeliveryFields {
		if !shopRequired(f.Key, 64) || !shopRequired(f.Label, 120) || seen[f.Key] {
			return ErrShopInvalid
		}
		seen[f.Key] = true
	}
	seen = map[string]bool{}
	for _, f := range t.BuyerInputSchema {
		if !slices.Contains([]string{"account_identifier", "contact_email", "order_note"}, f.Key) || !shopRequired(f.Label, 120) || seen[f.Key] {
			return ErrShopInvalid
		}
		seen[f.Key] = true
	}
	d := t.DeliveryPolicy
	if d.Mode != p.DeliveryType || !shopRequired(d.PublicTerms, 8000) {
		return ErrShopInvalid
	}
	if p.DeliveryType == "manual" {
		if err := ValidateShopSLA(d.SLA); err != nil {
			return err
		}
	} else if d.SLA != nil {
		return ErrShopInvalid
	}
	v := t.ViewPolicy
	if v.FirstStart != "delivered_at" || v.ReplacementStart != "delivered_at" || v.ExpiredAction != "deny_reveal" || !shopRequired(v.PublicTerms, 8000) {
		return ErrShopInvalid
	}
	if _, err := shopTimeAdd(1, v.DurationSeconds); err != nil {
		return err
	}
	if err := shopValidatePeriod(t.WarrantyPolicy, true); err != nil {
		return err
	}
	if err := shopValidatePeriod(t.EntitlementPolicy, false); err != nil {
		return err
	}
	if t.RefundPolicy.GoodsRefundMode != "full_only" || t.RefundPolicy.Execution != "manual_original_channel" || !shopRequired(t.RefundPolicy.PublicTerms, 8000) {
		return ErrShopInvalid
	}
	if p.DeliveryType == "automatic" && (!shopRequired(p.InventoryIdentityDomain, 128) || p.IdentityNormalizationVersion <= 0) {
		return ErrShopInvalid
	}
	return nil
}
func GetShopSettings(db *gorm.DB) (*ShopSettings, error) {
	var s ShopSettings
	err := db.First(&s, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Reading an unconfigured shop never opens sales or writes configuration.
		retention, err := common.Marshal(ShopRetentionPolicy{SecretAfterDeliverySeconds: 90 * 86400, BuyerInputAfterClosureSeconds: 30 * 86400, EvidenceAfterSettlementSeconds: 365 * 86400, AuditAfterSettlementSeconds: 1095 * 86400, IdempotencyAfterClosureSeconds: 365 * 86400})
		if err != nil {
			return nil, err
		}
		reminders, err := common.Marshal(ShopReminderPolicy{PaymentUnknownSeconds: 300, DeliveryExceptionSeconds: 900, RefundVerificationSeconds: 86400})
		if err != nil {
			return nil, err
		}
		return &ShopSettings{ID: 1, Provider: "epay", Environment: "production", Currency: "CNY", CurrencyExponent: 2, MinAmountMinor: 1, MaxAmountMinor: 1000000, ReservationSeconds: 900, MaxUnpaidOrders: 3, RetentionPolicyJSON: string(retention), ReminderPolicyJSON: string(reminders)}, nil
	}
	return &s, err
}
func UpdateShopSettings(db *gorm.DB, c ShopCommand, value ShopSettings, reason string) (*ShopSettings, error) {
	var result ShopSettings
	err := shopTransaction(db, func(tx *gorm.DB) error {
		if err := shopAuthorize(tx, c, true); err != nil {
			return err
		}
		k, e := shopIdempotency(tx, c, "settings", "1")
		if e != nil {
			return e
		}
		if k != nil {
			return tx.First(&result, 1).Error
		}
		if !shopRequired(reason, 200) {
			return ErrShopInvalid
		}
		if value.ReservationSeconds <= 0 || value.ReservationSeconds > 86400 || value.MaxUnpaidOrders <= 0 || value.MaxUnpaidOrders > 100 || value.MinAmountMinor <= 0 || value.MaxAmountMinor < value.MinAmountMinor || value.CurrencyExponent < 0 || value.CurrencyExponent > 6 || len(value.Currency) != 3 || value.Currency != strings.ToUpper(value.Currency) || !shopRequired(value.Provider, 50) || !shopRequired(value.ProviderAccountRef, 128) || !shopRequired(value.Environment, 32) {
			return ErrShopInvalid
		}
		var retention ShopRetentionPolicy
		var reminders ShopReminderPolicy
		if common.UnmarshalJsonStr(value.RetentionPolicyJSON, &retention) != nil || common.UnmarshalJsonStr(value.ReminderPolicyJSON, &reminders) != nil {
			return ErrShopInvalid
		}
		for _, n := range []int64{retention.SecretAfterDeliverySeconds, retention.BuyerInputAfterClosureSeconds, retention.EvidenceAfterSettlementSeconds, retention.AuditAfterSettlementSeconds, retention.IdempotencyAfterClosureSeconds, reminders.PaymentUnknownSeconds, reminders.DeliveryExceptionSeconds, reminders.RefundVerificationSeconds} {
			if _, e := shopTimeAdd(1, n); e != nil {
				return e
			}
		}
		var old ShopSettings
		e = lockForUpdate(tx).First(&old, 1).Error
		if errors.Is(e, gorm.ErrRecordNotFound) {
			if c.ExpectedVersion != 0 {
				return ErrShopVersion
			}
			value.ID = 1
			value.Version = 1
			value.UpdatedAt = shopNow(c.Now)
			if e = tx.Create(&value).Error; e != nil {
				return e
			}
		} else {
			if e != nil {
				return e
			}
			if e = shopVersion(old.Version, c.ExpectedVersion); e != nil {
				return e
			}
			value.ID = 1
			value.Version = old.Version + 1
			value.UpdatedAt = shopNow(c.Now)
			r := tx.Model(&ShopSettings{}).Where("id = 1 AND version = ?", old.Version).Select("*").Omit("id").Updates(&value)
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected != 1 {
				return ErrShopVersion
			}
		}
		result = value
		if e = shopEvent(tx, c, nil, nil, "settings_updated", "internal", reason, nil); e != nil {
			return e
		}
		return shopRemember(tx, c, "settings", "1", "settings", "1", nil)
	})
	return &result, err
}
func SaveShopCategory(db *gorm.DB, c ShopCommand, publicID, name string, sort int) (*ShopCategory, error) {
	var r ShopCategory
	err := shopTransaction(db, func(tx *gorm.DB) error {
		if e := shopAuthorize(tx, c, true); e != nil {
			return e
		}
		if !shopRequired(name, 120) {
			return ErrShopInvalid
		}
		k, e := shopIdempotency(tx, c, "category", publicID)
		if e != nil {
			return e
		}
		if k != nil {
			return tx.Where("public_id = ?", k.ResourcePublicID).First(&r).Error
		}
		if publicID == "" {
			rec, e := shopRecord("sc_", shopNow(c.Now))
			if e != nil {
				return e
			}
			r = ShopCategory{ShopRecord: rec, TenantID: PlatformTenantID, Name: name, SortOrder: sort}
			if e = tx.Create(&r).Error; e != nil {
				return e
			}
		} else {
			if e = lockForUpdate(tx).Where("public_id = ? AND tenant_id = ?", publicID, PlatformTenantID).First(&r).Error; e != nil {
				return shopNotFound(e)
			}
			if e = shopVersion(r.Version, c.ExpectedVersion); e != nil {
				return e
			}
			if e = shopCAS(tx, &ShopCategory{}, r.ID, r.Version, shopNow(c.Now), map[string]any{"name": name, "sort_order": sort}); e != nil {
				return e
			}
			r.Name = name
			r.SortOrder = sort
			r.Version++
		}
		return shopRemember(tx, c, "category", publicID, "category", r.PublicID, nil)
	})
	return &r, err
}
func SaveShopProduct(db *gorm.DB, c ShopCommand, publicID string, value ShopProduct) (*ShopProduct, error) {
	var r ShopProduct
	err := shopTransaction(db, func(tx *gorm.DB) error {
		if e := shopAuthorize(tx, c, true); e != nil {
			return e
		}
		k, e := shopIdempotency(tx, c, "product", publicID)
		if e != nil {
			return e
		}
		if k != nil {
			return tx.Where("public_id = ?", k.ResourcePublicID).First(&r).Error
		}
		if e = ValidateShopProduct(&value, false); e != nil {
			return e
		}
		if value.CategoryID != nil {
			var n int64
			if e = tx.Model(&ShopCategory{}).Where("id = ? AND tenant_id = ?", *value.CategoryID, PlatformTenantID).Count(&n).Error; e != nil {
				return e
			}
			if n != 1 {
				return ErrShopInvalid
			}
		}
		if publicID == "" {
			value.ShopRecord, e = shopRecord("sp_", shopNow(c.Now))
			if e != nil {
				return e
			}
			value.Status = "draft"
			value.TenantID = PlatformTenantID
			value.TermsVersion = 1
			value.ManualUsed = 0
			if e = tx.Create(&value).Error; e != nil {
				return e
			}
			r = value
		} else {
			if e = lockForUpdate(tx).Where("public_id = ? AND tenant_id = ?", publicID, PlatformTenantID).First(&r).Error; e != nil {
				return shopNotFound(e)
			}
			if e = shopVersion(r.Version, c.ExpectedVersion); e != nil {
				return e
			}
			if value.DeliveryType != r.DeliveryType || value.InventoryIdentityDomain != r.InventoryIdentityDomain || value.IdentityNormalizationVersion != r.IdentityNormalizationVersion {
				var n, m int64
				if e = tx.Model(&ShopOrder{}).Where("product_id = ?", r.ID).Count(&n).Error; e != nil {
					return e
				}
				if e = tx.Model(&ShopStockUnit{}).Where("product_id = ?", r.ID).Count(&m).Error; e != nil {
					return e
				}
				if n+m > 0 {
					return ErrShopAction
				}
			}
			if value.ManualCapacity < r.ManualUsed {
				return ErrShopCapacity
			}
			value.ShopRecord = r.ShopRecord
			value.TenantID = r.TenantID
			value.Status = r.Status
			value.ManualUsed = r.ManualUsed
			value.TermsVersion = r.TermsVersion
			if value.TermsJSON != r.TermsJSON {
				value.TermsVersion++
			}
			if r.Status == "published" {
				if e = ValidateShopProduct(&value, true); e != nil {
					return e
				}
			}
			value.Version++
			value.UpdatedAt = shopNow(c.Now)
			q := tx.Model(&ShopProduct{}).Where("id = ? AND version = ?", r.ID, r.Version).Select("*").Omit("id", "public_id", "created_at").Updates(&value)
			if q.Error != nil {
				return q.Error
			}
			if q.RowsAffected != 1 {
				return ErrShopVersion
			}
			r = value
		}
		if e = shopEvent(tx, c, nil, nil, "product_saved", "internal", r.PublicID, nil); e != nil {
			return e
		}
		return shopRemember(tx, c, "product", publicID, "product", r.PublicID, nil)
	})
	return &r, err
}
func SetShopProductStatus(db *gorm.DB, c ShopCommand, publicID, status, reason string) (*ShopProduct, error) {
	var r ShopProduct
	err := shopTransaction(db, func(tx *gorm.DB) error {
		if e := shopAuthorize(tx, c, true); e != nil {
			return e
		}
		if !slices.Contains([]string{"published", "unpublished", "archived"}, status) {
			return ErrShopInvalid
		}
		k, e := shopIdempotency(tx, c, "product-status", publicID)
		if e != nil {
			return e
		}
		if k != nil {
			return tx.Where("public_id = ?", k.ResourcePublicID).First(&r).Error
		}
		var s ShopSettings
		if status == "published" {
			if e = lockForUpdate(tx).First(&s, 1).Error; e != nil {
				return ErrShopPaused
			}
		}
		if e = lockForUpdate(tx).Where("public_id = ? AND tenant_id = ?", publicID, PlatformTenantID).First(&r).Error; e != nil {
			return shopNotFound(e)
		}
		if e = shopVersion(r.Version, c.ExpectedVersion); e != nil {
			return e
		}
		if status == "published" {
			if e = ValidateShopProduct(&r, true); e != nil {
				return e
			}
			if r.Currency != s.Currency || r.CurrencyExponent != s.CurrencyExponent || r.PriceMinor < s.MinAmountMinor || r.PriceMinor > s.MaxAmountMinor {
				return ErrShopInvalid
			}
		} else if !shopRequired(reason, 2000) {
			return ErrShopInvalid
		}
		if e = shopCAS(tx, &ShopProduct{}, r.ID, r.Version, shopNow(c.Now), map[string]any{"status": status}); e != nil {
			return e
		}
		r.Status = status
		r.Version++
		if e = shopEvent(tx, c, nil, nil, "product_"+status, "internal", reason, nil); e != nil {
			return e
		}
		return shopRemember(tx, c, "product-status", publicID, "product", r.PublicID, nil)
	})
	return &r, err
}
func ListShopProducts(db *gorm.DB, scope ShopScope, status string, categoryID *int64, query string, page, limit int) ([]ShopProduct, int64, error) {
	if scope.TenantID != PlatformTenantID {
		return nil, 0, ErrShopNotFound
	}
	if scope.Admin {
		if err := shopReadScope(db, scope, true); err != nil {
			return nil, 0, err
		}
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	if page > 1000000 {
		return nil, 0, ErrShopInvalid
	}
	q := db.Model(&ShopProduct{}).Where("tenant_id = ?", PlatformTenantID)
	if !scope.Admin {
		q = q.Where("status = ?", "published")
	} else if status != "" {
		q = q.Where("status = ?", status)
	}
	if categoryID != nil {
		q = q.Where("category_id = ?", *categoryID)
	}
	if query != "" {
		if !shopBoundedText(query, 120) {
			return nil, 0, ErrShopInvalid
		}
		q = q.Where("name LIKE ?", "%"+strings.ReplaceAll(strings.ReplaceAll(query, "%", ""), "_", "")+"%")
	}
	var n int64
	if e := q.Count(&n).Error; e != nil {
		return nil, 0, e
	}
	var rows []ShopProduct
	e := q.Order("sort_order asc, id desc").Offset((page - 1) * limit).Limit(limit).Find(&rows).Error
	return rows, n, e
}
func GetShopProduct(db *gorm.DB, scope ShopScope, id string) (*ShopProduct, error) {
	q := db.Where("public_id = ? AND tenant_id = ?", id, PlatformTenantID)
	if scope.TenantID != PlatformTenantID {
		return nil, ErrShopNotFound
	}
	if scope.Admin {
		if err := shopReadScope(db, scope, true); err != nil {
			return nil, err
		}
	}
	if !scope.Admin {
		q = q.Where("status = ?", "published")
	}
	var p ShopProduct
	e := q.First(&p).Error
	return &p, shopNotFound(e)
}
func ListShopCategories(db *gorm.DB) ([]ShopCategory, error) {
	var rows []ShopCategory
	e := db.Where("tenant_id = ?", PlatformTenantID).Order("sort_order asc, id asc").Limit(1000).Find(&rows).Error
	return rows, e
}

type ShopFingerprint struct {
	KeyVersion string
	Value      string
}
type ShopStockImportRow struct {
	PublicID               string
	Secret                 ShopSecret
	DomainHash             string
	Fingerprint            string
	FingerprintKeyVersion  string
	HistoricalFingerprints []ShopFingerprint
	NormalizationVersion   int64
	MaskedLabel            string
}
type ShopImportInput struct {
	Command         ShopCommand
	ProductPublicID string
	ProductVersion  int64
	TemplateVersion int64
	Rows            []ShopStockImportRow
}
type ShopImportResult struct {
	BatchID       string
	InsertedCount int
}

func ImportShopInventory(db *gorm.DB, in ShopImportInput) (*ShopImportResult, error) {
	var result ShopImportResult
	c := in.Command
	err := shopTransaction(db, func(tx *gorm.DB) error {
		if e := shopAuthorize(tx, c, true); e != nil {
			return e
		}
		k, e := shopIdempotency(tx, c, "inventory-import", in.ProductPublicID)
		if e != nil {
			return e
		}
		if k != nil {
			return common.UnmarshalJsonStr(k.ResultMetadataJSON, &result)
		}
		if len(in.Rows) == 0 || len(in.Rows) > 200 || c.HashKeyVersion == "" {
			return ErrShopInvalid
		}
		var p ShopProduct
		if e = lockForUpdate(tx).Where("public_id = ? AND tenant_id = ?", in.ProductPublicID, PlatformTenantID).First(&p).Error; e != nil {
			return shopNotFound(e)
		}
		if e = shopVersion(p.Version, in.ProductVersion); e != nil {
			return e
		}
		var terms ShopProductTerms
		if common.UnmarshalJsonStr(p.TermsJSON, &terms) != nil || terms.TemplateVersion != in.TemplateVersion || p.DeliveryType != "automatic" {
			return ErrShopInvalid
		}
		batch, e := NewShopPublicID("sb_")
		if e != nil {
			return e
		}
		var units []ShopStockUnit
		seen := map[string]bool{}
		for _, row := range in.Rows {
			if row.PublicID == "" || row.Secret.Ciphertext == "" || row.Secret.KeyVersion == "" || len(row.DomainHash) != 64 || len(row.Fingerprint) != 64 || !shopRequired(row.FingerprintKeyVersion, 32) || row.NormalizationVersion != p.IdentityNormalizationVersion || !shopBoundedText(row.MaskedLabel, 200) {
				return ErrShopInvalid
			}
			identity := row.DomainHash + row.FingerprintKeyVersion + row.Fingerprint
			if seen[identity] {
				return ErrShopStock
			}
			seen[identity] = true
			fingerprints := append([]ShopFingerprint{{KeyVersion: row.FingerprintKeyVersion, Value: row.Fingerprint}}, row.HistoricalFingerprints...)
			if len(fingerprints) > 16 {
				return ErrShopInvalid
			}
			for _, fp := range fingerprints {
				var n int64
				if e = tx.Model(&ShopStockUnit{}).Where("identity_domain_hash = ? AND fingerprint_key_version = ? AND identity_fingerprint = ?", row.DomainHash, fp.KeyVersion, fp.Value).Count(&n).Error; e != nil {
					return e
				}
				if n > 0 {
					return ErrShopStock
				}
			}
			units = append(units, ShopStockUnit{ShopRecord: ShopRecord{PublicID: row.PublicID, Version: 1, CreatedAt: shopNow(c.Now), UpdatedAt: shopNow(c.Now)}, TenantID: PlatformTenantID, ProductID: p.ID, State: "available", SecretCiphertext: row.Secret.Ciphertext, SecretKeyVersion: row.Secret.KeyVersion, FingerprintKeyVersion: row.FingerprintKeyVersion, IdentityFingerprint: row.Fingerprint, IdentityDomainHash: row.DomainHash, ImportBatchPublicID: batch, NormalizationVersion: row.NormalizationVersion, MaskedLabel: row.MaskedLabel})
		}
		if e = tx.CreateInBatches(&units, 100).Error; e != nil {
			return e
		}
		result = ShopImportResult{BatchID: batch, InsertedCount: len(units)}
		if e = shopEvent(tx, c, nil, nil, "inventory_imported", "internal", fmt.Sprintf("%s: %d", batch, len(units)), nil); e != nil {
			return e
		}
		return shopRemember(tx, c, "inventory-import", in.ProductPublicID, "inventory_batch", batch, result)
	})
	return &result, err
}
