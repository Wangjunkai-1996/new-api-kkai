package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type TenantSupplyEntry struct {
	Model              string `json:"model"`
	SalesGroup         string `json:"sales_group"`
	SupplierPriceMinor int64  `json:"supplier_price_minor"`
	MinimumMultiplier  string `json:"minimum_multiplier"`
	PriceUnit          string `json:"price_unit"`
}

func ListTenantSupply(db *gorm.DB) ([]TenantSupplyEntry, error) {
	var abilities []model.Ability
	if err := db.Where("enabled = ?", true).Find(&abilities).Error; err != nil {
		return nil, err
	}
	entries := make([]TenantSupplyEntry, 0, len(abilities))
	supported, err := tenantTextSupplyModels(db)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	openGroups := setting.GetUserUsableGroupsCopy()
	for _, ability := range abilities {
		key := ability.Group + "\x00" + ability.Model
		if _, open := openGroups[ability.Group]; seen[key] || !open || IsAutoGroup(ability.Group) || !ratio_setting.ContainsGroupRatio(ability.Group) || !supported[key] {
			continue
		}
		seen[key] = true
		price, unit, err := TenantSupplierUnitPrice(ability.Model, ability.Group)
		if err != nil {
			continue
		}
		minimum, err := EffectiveTenantMultiplier("0", ability.Group)
		if err != nil {
			continue
		}
		entries = append(entries, TenantSupplyEntry{Model: ability.Model, SalesGroup: ability.Group, SupplierPriceMinor: price, MinimumMultiplier: minimum.String(), PriceUnit: unit})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].SalesGroup == entries[j].SalesGroup {
			return entries[i].Model < entries[j].Model
		}
		if entries[i].SalesGroup == "default" {
			return true
		}
		if entries[j].SalesGroup == "default" {
			return false
		}
		return entries[i].SalesGroup < entries[j].SalesGroup
	})
	return entries, nil
}

// tenantTextSupplyModels uses the platform's resolved provider and endpoint
// metadata. Task billing and media endpoints need their own cash mapping.
func tenantTextSupplyModels(db *gorm.DB) (map[string]bool, error) {
	var abilities []model.Ability
	if err := db.Table("abilities").Select("abilities.*").
		Joins("JOIN channels ON channels.id = abilities.channel_id").
		Where("abilities.enabled = ? AND channels.status = ? AND channels.type IN ?", true, common.ChannelStatusEnabled, constant.TenantTextChannelTypes()).
		Find(&abilities).Error; err != nil {
		return nil, err
	}
	transports := make(map[string]bool, len(abilities))
	for _, ability := range abilities {
		transports[ability.Group+"\x00"+ability.Model] = true
	}
	supported := make(map[string]bool)
	for _, row := range model.GetPricing() {
		if !row.PricingConfigured || len(row.BillingUsageSchema) > 0 || len(row.BillingPluginVariants) > 0 || row.BillingUnit != "" {
			continue
		}
		text, unsupported := false, false
		for _, endpoint := range row.SupportedEndpointTypes {
			switch endpoint {
			case relaytypes.EndpointTypeOpenAI, relaytypes.EndpointTypeOpenAIResponse, relaytypes.EndpointTypeAnthropic, relaytypes.EndpointTypeEmbeddings, relaytypes.EndpointTypeGemini:
				text = true
			case relaytypes.EndpointTypeOpenAIResponseCompact, relaytypes.EndpointTypeOpenAIAlphaSearch:
				// Native textual provider surfaces may coexist with compatibility.
			default:
				unsupported = true
			}
		}
		if !text || unsupported {
			continue
		}
		for _, group := range row.EnableGroup {
			key := group + "\x00" + row.ModelName
			if transports[key] {
				supported[key] = true
			}
		}
	}
	return supported, nil
}

// TenantSupplierUnitPrice projects platform pricing for display. Cash income
// is always allocated from paid lots; this projection never funds a wallet.
func TenantSupplierUnitPrice(modelName, group string) (int64, string, error) {
	if billing_setting.GetBillingMode(modelName) == billing_setting.BillingModeTieredExpr {
		// Expressions have no single unit price. Runtime quotas still use the
		// complete frozen platform expression and the selected group ratio.
		return 0, "expression", nil
	}
	price, perCall := ratio_setting.GetModelPrice(modelName, false)
	unit := "per_call"
	if !perCall {
		ratio, found, _ := ratio_setting.GetModelRatio(modelName)
		if !found {
			return 0, "", model.ErrResellerInvalidPrice
		}
		price = ratio * 2
		unit = "per_million_input_tokens"
	}
	if math.IsNaN(price) || math.IsInf(price, 0) || math.IsNaN(operation_setting.Price) || math.IsInf(operation_setting.Price, 0) || math.IsNaN(ratio_setting.GetGroupRatio(group)) || math.IsInf(ratio_setting.GetGroupRatio(group), 0) || price < 0 || operation_setting.Price <= 0 {
		return 0, "", model.ErrResellerInvalidPrice
	}
	value := decimal.NewFromFloat(price).Mul(decimal.NewFromFloat(ratio_setting.GetGroupRatio(group))).Mul(decimal.NewFromFloat(operation_setting.Price)).Mul(decimal.NewFromInt(100)).Ceil()
	if value.IsNegative() || value.GreaterThan(decimal.NewFromInt(1<<60)) {
		return 0, "", model.ErrResellerInvalidPrice
	}
	return value.IntPart(), unit, nil
}

// ResolveCatalog inherits the current public, supported platform supply. Legacy
// selections remain stored but do not prevent newly opened models from appearing.
func (s *TenantPricingService) ResolveCatalog(ctx context.Context, tenantID int64) ([]model.TenantCatalogEntry, error) {
	if s == nil || s.db == nil || tenantID <= 0 {
		return nil, model.ErrResellerInvalidCatalog
	}
	supply, err := ListTenantSupply(s.db.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	entries := make([]model.TenantCatalogEntry, 0, len(supply))
	for i, entry := range supply {
		entries = append(entries, model.TenantCatalogEntry{TenantID: tenantID, Model: entry.Model, SalesGroup: entry.SalesGroup, Status: model.TenantCatalogStatusEnabled, IsDefault: i == 0, SortOrder: i})
	}
	return entries, nil
}

func (s *TenantPricingService) ResolveCurrentPrice(ctx context.Context, tenantID int64, modelName, group string) (*model.TenantPriceVersion, error) {
	entries, err := s.ResolveCatalog(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	available := false
	for _, entry := range entries {
		available = available || (entry.Model == modelName && entry.SalesGroup == group)
	}
	if !available {
		return nil, model.ErrResellerInvalidCatalog
	}
	groups, err := s.ListGroupPrices(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	for _, price := range groups {
		if price.Group != group {
			continue
		}
		cost, _, err := TenantSupplierUnitPrice(modelName, group)
		if err != nil {
			return nil, err
		}
		return &model.TenantPriceVersion{ID: price.ID, TenantID: tenantID, Model: modelName, Group: group, Version: price.Version, SalesMultiplier: price.SalesMultiplier, SupplierPriceMinor: cost}, nil
	}
	return nil, model.ErrResellerInvalidPrice
}

// EffectiveTenantMultiplier keeps new quotes at the current platform supply
// floor. Payment fees affect real profit, never an implicit customer markup.
func EffectiveTenantMultiplier(multiplier string, group string) (decimal.Decimal, error) {
	ratio, err := decimal.NewFromString(multiplier)
	if len(multiplier) > 64 || err != nil || ratio.Exponent() < -32 || ratio.Exponent() > 6 || ratio.IsNegative() || ratio.GreaterThan(decimal.NewFromInt(1000000)) {
		return decimal.Zero, model.ErrResellerInvalidPrice
	}
	supply := ratio_setting.GetGroupRatio(group)
	if math.IsNaN(supply) || math.IsInf(supply, 0) || supply < 0 {
		return decimal.Zero, model.ErrResellerInvalidPrice
	}
	cost := decimal.NewFromFloat(supply)
	if ratio.GreaterThanOrEqual(cost) {
		return ratio, nil
	}
	minimum := cost
	if minimum.GreaterThan(decimal.NewFromInt(1000000)) {
		return decimal.Zero, model.ErrResellerInvalidPrice
	}
	return minimum, nil
}

func (s *TenantPricingService) ValidateMinimumPrice(multiplier string, group string) error {
	effective, err := EffectiveTenantMultiplier(multiplier, group)
	if err != nil {
		return err
	}
	requested, _ := decimal.NewFromString(multiplier)
	if !effective.Equal(requested) {
		return model.ErrResellerPriceBelowCost
	}
	return nil
}

func (s *TenantPricingService) SaveCatalog(ctx context.Context, tenantID int64, entries []model.TenantCatalogEntry) error {
	if err := model.ValidateTenantCatalog(entries); err != nil {
		return err
	}
	group := entries[0].SalesGroup
	supply, err := ListTenantSupply(s.db)
	if err != nil {
		return err
	}
	allowed := make(map[string]bool, len(supply))
	for _, entry := range supply {
		allowed[entry.SalesGroup+"\x00"+entry.Model] = true
	}
	for _, entry := range entries {
		if entry.TenantID != tenantID || entry.SalesGroup != group || !allowed[entry.SalesGroup+"\x00"+entry.Model] {
			return model.ErrResellerInvalidCatalog
		}
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := model.LockTenantTx(tx, tenantID); err != nil {
			return err
		}
		if err := tx.Where("tenant_id = ?", tenantID).Delete(&model.TenantCatalogEntry{}).Error; err != nil {
			return err
		}
		return tx.Create(&entries).Error
	})
}

type TenantPriceUpdate struct {
	Model                      string
	Group                      string
	Multiplier                 string
	MarkupPercent              *string
	ConvertLegacy              bool
	ExpectedPlatformMultiplier string
	ExpectedVersion            int64
}

var ErrTenantPriceVersionConflict = errors.New("tenant price version conflict")

func (s *TenantPricingService) SavePrices(ctx context.Context, tenantID int64, updates []TenantPriceUpdate) ([]model.TenantPriceVersion, error) {
	if len(updates) == 0 || len(updates) > 100 {
		return nil, model.ErrResellerInvalidPrice
	}
	prices := make([]model.TenantPriceVersion, 0, len(updates))
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := model.LockTenantTx(tx, tenantID); err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, update := range updates {
			key := update.Group + "\x00" + update.Model
			if seen[key] || strings.TrimSpace(update.Model) == "" {
				return model.ErrResellerInvalidPrice
			}
			seen[key] = true
			if err := s.ValidateMinimumPrice(update.Multiplier, update.Group); err != nil {
				return err
			}
			var entry model.TenantCatalogEntry
			if err := tx.Where("tenant_id = ? AND model = ? AND sales_group = ? AND status = ?", tenantID, update.Model, update.Group, model.TenantCatalogStatusEnabled).First(&entry).Error; err != nil {
				return err
			}
			var previous model.TenantPriceVersion
			err := tx.Where(map[string]any{"tenant_id": tenantID, "model": update.Model, "group": update.Group}).Order("version DESC, id DESC").First(&previous).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if previous.Version != update.ExpectedVersion {
				return ErrTenantPriceVersionConflict
			}
			cost, _, err := TenantSupplierUnitPrice(update.Model, update.Group)
			if err != nil {
				return err
			}
			price := model.TenantPriceVersion{TenantID: tenantID, Model: update.Model, Group: update.Group, Version: previous.Version + 1, SalesMultiplier: update.Multiplier, SupplierPriceMinor: cost, EffectiveAt: time.Now().Unix(), CreatedAt: time.Now().Unix()}
			if err := tx.Create(&price).Error; err != nil {
				return fmt.Errorf("save tenant price: %w", err)
			}
			prices = append(prices, price)
		}
		return nil
	})
	return prices, err
}

// TenantGroupPrice applies to every current and future model in an open group.
// Empty stored multipliers are immutable reset events, not a zero sale price.
type TenantGroupPrice struct {
	ID                 int64  `json:"id"`
	Group              string `json:"group"`
	SalesMultiplier    string `json:"sales_multiplier"`
	PlatformMultiplier string `json:"platform_multiplier"`
	MinimumMultiplier  string `json:"minimum_multiplier"`
	MarkupPercent      string `json:"markup_percent"`
	PricingMode        string `json:"pricing_mode"`
	Version            int64  `json:"version"`
	Inherited          bool   `json:"inherited"`
}

func (s *TenantPricingService) ListGroupPrices(ctx context.Context, tenantID int64) ([]TenantGroupPrice, error) {
	if s == nil || s.db == nil || tenantID <= 0 {
		return nil, model.ErrResellerInvalidPrice
	}
	supply, err := ListTenantSupply(s.db.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	latest := s.db.WithContext(ctx).Model(&model.TenantPriceVersion{}).Select("MAX(id)").Where("tenant_id = ? AND model = ?", tenantID, "").Group("group")
	var versions []model.TenantPriceVersion
	if err := s.db.WithContext(ctx).Where("id IN (?)", latest).Find(&versions).Error; err != nil {
		return nil, err
	}
	byGroup := make(map[string]model.TenantPriceVersion, len(versions))
	for _, version := range versions {
		byGroup[version.Group] = version
	}
	prices := make([]TenantGroupPrice, 0)
	seen := make(map[string]bool)
	for _, entry := range supply {
		if seen[entry.SalesGroup] {
			continue
		}
		seen[entry.SalesGroup] = true
		version := byGroup[entry.SalesGroup]
		platformRatio, err := EffectiveTenantMultiplier("0", entry.SalesGroup)
		if err != nil {
			return nil, err
		}
		platform := platformRatio.String()
		effective, markup, mode, err := resolveTenantGroupMarkup(version.SalesMultiplier, platformRatio)
		if err != nil {
			return nil, err
		}
		prices = append(prices, TenantGroupPrice{ID: version.ID, Group: entry.SalesGroup, SalesMultiplier: effective.String(), PlatformMultiplier: platform, MinimumMultiplier: platform, MarkupPercent: markup, PricingMode: mode, Version: version.Version, Inherited: version.SalesMultiplier == ""})
	}
	return prices, nil
}

// ResolveTokenGroup uses the same public supply as the tenant's group picker.
// Platform account-specific group grants do not apply to tenant customers.
func (s *TenantPricingService) ResolveTokenGroup(ctx context.Context, tenantID int64, userGroup, tokenGroup string) (string, error) {
	group := tokenGroup
	if group == "" {
		group = userGroup
	}
	if group == "" || IsAutoGroup(group) {
		return "", model.ErrResellerInvalidCatalog
	}
	prices, err := s.ListGroupPrices(ctx, tenantID)
	if err != nil {
		return "", err
	}
	for _, price := range prices {
		if price.Group == group {
			return group, nil
		}
	}
	return "", model.ErrResellerInvalidCatalog
}

func (s *TenantPricingService) SaveGroupPrices(ctx context.Context, tenantID int64, updates []TenantPriceUpdate) ([]TenantGroupPrice, error) {
	if s == nil || s.db == nil || tenantID <= 0 || len(updates) == 0 || len(updates) > 100 {
		return nil, model.ErrResellerInvalidPrice
	}
	available, err := ListTenantSupply(s.db.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	open := make(map[string]bool, len(available))
	for _, price := range available {
		open[price.SalesGroup] = true
	}
	var prices []TenantGroupPrice
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := model.LockTenantTx(tx, tenantID); err != nil {
			return err
		}
		seen := make(map[string]bool, len(updates))
		for _, update := range updates {
			if update.Model != "" || !open[update.Group] || seen[update.Group] || update.ExpectedVersion < 0 {
				return model.ErrResellerInvalidPrice
			}
			seen[update.Group] = true
			var previous model.TenantPriceVersion
			err := tx.Where(map[string]any{"tenant_id": tenantID, "model": "", "group": update.Group}).Order("version DESC, id DESC").First(&previous).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if previous.Version != update.ExpectedVersion {
				return ErrTenantPriceVersionConflict
			}
			if previous.Version == math.MaxInt64 {
				return model.ErrResellerInvalidPrice
			}
			stored := update.Multiplier
			if update.MarkupPercent != nil || update.ConvertLegacy {
				if update.Multiplier != "" || (update.MarkupPercent != nil && update.ConvertLegacy) {
					return model.ErrResellerInvalidPrice
				}
				platform, err := EffectiveTenantMultiplier("0", update.Group)
				if err != nil {
					return err
				}
				if update.ConvertLegacy {
					if update.ExpectedPlatformMultiplier == "" || len(update.ExpectedPlatformMultiplier) > 512 {
						return model.ErrResellerInvalidPrice
					}
					expected, err := decimal.NewFromString(update.ExpectedPlatformMultiplier)
					if err != nil || expected.Exponent() < -324 || expected.Exponent() > 6 || expected.IsNegative() || expected.GreaterThan(decimal.NewFromInt(1000000)) {
						return model.ErrResellerInvalidPrice
					}
					if !expected.Equal(platform) {
						return ErrTenantPriceVersionConflict
					}
					effective, _, mode, err := resolveTenantGroupMarkup(previous.SalesMultiplier, platform)
					if err != nil || mode != "legacy" {
						return model.ErrResellerInvalidPrice
					}
					if platform.IsZero() {
						if !effective.IsZero() {
							return model.ErrResellerInvalidPrice
						}
						stored = ""
					} else {
						stored, err = encodeTenantMarkupFactor(new(big.Rat).Quo(effective.Rat(), platform.Rat()))
						if err != nil {
							return err
						}
					}
				} else {
					stored, err = tenantMarkupFromPercent(*update.MarkupPercent)
					if err != nil {
						return err
					}
				}
				if _, _, _, err := resolveTenantGroupMarkup(stored, platform); err != nil {
					return err
				}
			} else if stored != "" {
				if err := s.ValidateMinimumPrice(stored, update.Group); err != nil {
					return err
				}
			}
			price := model.TenantPriceVersion{TenantID: tenantID, Group: update.Group, Version: previous.Version + 1, SalesMultiplier: stored, EffectiveAt: time.Now().Unix(), CreatedAt: time.Now().Unix()}
			if err := tx.Create(&price).Error; err != nil {
				return err
			}
		}
		// Validate and return the same transaction snapshot. An invalid sibling
		// group must not turn a committed change into an error response.
		prices, err = NewTenantPricingService(tx).ListGroupPrices(ctx, tenantID)
		return err
	})
	return prices, err
}
