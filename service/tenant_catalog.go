package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
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
	for _, ability := range abilities {
		key := ability.Group + "\x00" + ability.Model
		if seen[key] || ability.Group == "auto" || !supported[key] {
			continue
		}
		seen[key] = true
		price, unit, err := TenantSupplierUnitPrice(ability.Model, ability.Group)
		if err != nil {
			continue
		}
		minimum, err := EffectiveTenantMultiplier("0.000000000000000001", ability.Group)
		if err != nil {
			continue
		}
		entries = append(entries, TenantSupplyEntry{Model: ability.Model, SalesGroup: ability.Group, SupplierPriceMinor: price, MinimumMultiplier: minimum.String(), PriceUnit: unit})
	}
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
			case relaytypes.EndpointTypeOpenAI, relaytypes.EndpointTypeOpenAIResponse, relaytypes.EndpointTypeAnthropic, relaytypes.EndpointTypeEmbeddings:
				text = true
			case relaytypes.EndpointTypeGemini, relaytypes.EndpointTypeOpenAIResponseCompact, relaytypes.EndpointTypeOpenAIAlphaSearch:
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

func (s *TenantPricingService) ResolveCatalog(ctx context.Context, tenantID int64) ([]model.TenantCatalogEntry, error) {
	if s == nil || s.db == nil || tenantID <= 0 {
		return nil, model.ErrResellerInvalidCatalog
	}
	var entries []model.TenantCatalogEntry
	err := s.db.WithContext(ctx).Where("tenant_id = ? AND status = ?", tenantID, model.TenantCatalogStatusEnabled).Order("sort_order, id").Find(&entries).Error
	return entries, err
}

func (s *TenantPricingService) ResolveCurrentPrice(ctx context.Context, tenantID int64, modelName, group string) (*model.TenantPriceVersion, error) {
	if s == nil || s.db == nil || tenantID <= 0 {
		return nil, model.ErrResellerInvalidPrice
	}
	supported, err := tenantTextSupplyModels(s.db.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	if !supported[group+"\x00"+modelName] {
		return nil, model.ErrResellerInvalidCatalog
	}
	var entry model.TenantCatalogEntry
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND model = ? AND sales_group = ? AND status = ?", tenantID, modelName, group, model.TenantCatalogStatusEnabled).First(&entry).Error; err != nil {
		return nil, err
	}
	var price model.TenantPriceVersion
	if err := s.db.WithContext(ctx).Where(map[string]any{"tenant_id": tenantID, "model": modelName, "group": group}).Order("version DESC, id DESC").First(&price).Error; err != nil {
		return nil, err
	}
	effective, err := EffectiveTenantMultiplier(price.SalesMultiplier, group)
	if err != nil {
		return nil, err
	}
	price.SalesMultiplier = effective.String()
	return &price, nil
}

// EffectiveTenantMultiplier raises new quotes to the current fee-inclusive
// supply floor. Stored price versions and already authorized quotes stay intact.
func EffectiveTenantMultiplier(multiplier string, group string) (decimal.Decimal, error) {
	ratio, err := decimal.NewFromString(multiplier)
	if len(multiplier) > 64 || err != nil || ratio.Exponent() < -18 || ratio.Exponent() > 6 || !ratio.IsPositive() || ratio.GreaterThan(decimal.NewFromInt(1000000)) {
		return decimal.Zero, model.ErrResellerInvalidPrice
	}
	supply := ratio_setting.GetGroupRatio(group)
	if math.IsNaN(supply) || math.IsInf(supply, 0) || supply < 0 {
		return decimal.Zero, model.ErrResellerInvalidPrice
	}
	net := decimal.RequireFromString("0.984")
	cost := decimal.NewFromFloat(supply)
	if ratio.Mul(net).GreaterThanOrEqual(cost) {
		return ratio, nil
	}
	minimum := cost.DivRound(net, 18)
	if minimum.Mul(net).LessThan(cost) {
		minimum = minimum.Add(decimal.New(1, -18))
	}
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
	Model           string
	Group           string
	Multiplier      string
	ExpectedVersion int64
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
