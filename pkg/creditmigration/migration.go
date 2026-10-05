// Package creditmigration contains the deliberately small, offline-only part
// of the KKAI credit-unit migration.  It does not import model.DB or any
// application caches.  The caller must provide a database and a maintenance
// contract; this keeps an accidental production invocation read-only.
package creditmigration

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	PlanVersion               = 2
	Numerator           int64 = 3
	Denominator         int64 = 40
	DefaultReceiptTable       = "kkai_credit_migration_receipts"
)

// ReceiptTableSchema returns the DDL that must be provisioned by the normal
// schema/release process.  The migration command intentionally refuses to
// create this table during a credit apply transaction.
func ReceiptTableSchema(table, dialect string) string {
	if !validIdentifier.MatchString(table) {
		table = DefaultReceiptTable
	}
	quote := "`"
	if strings.EqualFold(dialect, "postgres") || strings.EqualFold(dialect, "postgresql") {
		quote = `"`
	}
	textType := "text"
	if strings.EqualFold(dialect, "mysql") {
		textType = "longtext"
	}
	return fmt.Sprintf("CREATE TABLE %s%s%s (migration_id varchar(128) PRIMARY KEY, plan_hash varchar(128) NOT NULL, state varchar(16) NOT NULL, before_image %s NOT NULL, after_image %s NOT NULL, created_at bigint NOT NULL, updated_at bigint NOT NULL, pricing_plan_hash varchar(128) NOT NULL, options_before %s NOT NULL, options_after %s NOT NULL);\n", quote, table, quote, textType, textType, textType, textType)
}

var validIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var (
	ErrInvalidPlan         = errors.New("invalid credit migration plan")
	ErrMaintenanceRequired = errors.New("explicit offline maintenance contract is required")
	ErrPlanDrift           = errors.New("credit migration plan drift detected")
	ErrUnsupportedData     = errors.New("credit migration has unsupported data")
	ErrReceiptMissing      = errors.New("credit migration receipt table is missing")
	ErrFiniteLimitRounding = errors.New("finite quota limit would round to zero")
)

// MaintenanceContract is intentionally external to the application.  A
// production operator creates it only after the controller has proved that
// all writers are stopped and the database is in the offline migration state.
type MaintenanceContract struct {
	MigrationID       string `json:"migration_id"`
	Offline           bool   `json:"offline"`
	AllowApply        bool   `json:"allow_credit_migration"`
	MaintenanceLocked bool   `json:"maintenance_locked"`
	WritesStopped     bool   `json:"writes_stopped"`
	BackupReadable    bool   `json:"backup_readable"`
	LogDatabaseMode   string `json:"log_database_mode"`
}

func (c MaintenanceContract) Validate(migrationID string) error {
	if !c.Offline || !c.AllowApply || strings.TrimSpace(c.MigrationID) == "" || c.MigrationID != migrationID {
		return ErrMaintenanceRequired
	}
	return nil
}

type TableSnapshot struct {
	Table  string `json:"table"`
	Exists bool   `json:"exists"`
	Rows   int64  `json:"rows"`
	MaxID  int64  `json:"max_id"`
	Sum    int64  `json:"sum"`
}

type Inventory struct {
	MigrationID  string          `json:"migration_id"`
	GeneratedAt  int64           `json:"generated_at"`
	Tables       []TableSnapshot `json:"tables"`
	Unsupported  []string        `json:"unsupported"`
	MissingCore  []string        `json:"missing_core"`
	ReceiptReady bool            `json:"receipt_ready"`
}

// Entry is one row before/after image.  Values contain only fields owned by
// this tool; unrelated columns are never rewritten.
type Entry struct {
	Table  string            `json:"table"`
	ID     int64             `json:"id"`
	Before map[string]string `json:"before"`
	After  map[string]string `json:"after"`
}

type Plan struct {
	Version      int             `json:"version"`
	MigrationID  string          `json:"migration_id"`
	Numerator    int64           `json:"numerator"`
	Denominator  int64           `json:"denominator"`
	Rounding     string          `json:"rounding"`
	ReceiptTable string          `json:"receipt_table"`
	Source       []TableSnapshot `json:"source"`
	Entries      []Entry         `json:"entries"`
	Blockers     []string        `json:"blockers"`
	PlanHash     string          `json:"plan_hash"`
	GeneratedAt  int64           `json:"generated_at"`
}

type Receipt struct {
	MigrationID     string `json:"migration_id"`
	PlanHash        string `json:"plan_hash"`
	State           string `json:"state"`
	BeforeImage     string `json:"before_image"`
	AfterImage      string `json:"after_image"`
	CreatedAt       int64  `json:"created_at"`
	UpdatedAt       int64  `json:"updated_at"`
	PricingPlanHash string `json:"pricing_plan_hash"`
	OptionsBefore   string `json:"options_before"`
	OptionsAfter    string `json:"options_after"`
}

type columnSpec struct {
	table string
	cols  []string
	// finiteZero means a positive old value may not become zero: zero has a
	// distinct unlimited/no-code meaning for these columns.
	finiteZero map[string]bool
	required   bool
}

var specs = []columnSpec{
	{table: "users", cols: []string{"id", "quota", "used_quota", "aff_quota", "aff_history", "setting"}, required: true},
	{table: "tokens", cols: []string{"id", "remain_quota", "used_quota"}},
	{table: "redemptions", cols: []string{"id", "quota"}, finiteZero: map[string]bool{"quota": true}},
	{table: "subscription_plans", cols: []string{"id", "total_amount"}, finiteZero: map[string]bool{"total_amount": true}},
	{table: "user_subscriptions", cols: []string{"id", "amount_total", "amount_used"}, finiteZero: map[string]bool{"amount_total": true}},
	{table: "channels", cols: []string{"id", "used_quota"}},
}

// These tables contain history, payment facts, or an external/immutable
// ledger.  Until a matching adapter is implemented, non-empty data blocks
// apply instead of being silently treated as migrated.
var historyTables = []string{"logs", "quota_data", "checkins", "top_ups", "subscription_orders", "kkai_internal_balance_adjustments", "kkai_outbox", "tasks", "midjourneys", "kkai_image_generations", "subscription_pre_consume_records"}

var unsupportedTables = []string{
	"kkai_rebate_events", "affiliate_ledger", "redeem_issuer_events",
}

var unsupportedTableSet = func() map[string]bool {
	set := make(map[string]bool, len(unsupportedTables))
	for _, table := range append(append([]string{}, unsupportedTables...), historyTables...) {
		set[table] = true
	}
	return set
}()

func Convert(old int64) (int64, int64) {
	if old == 0 {
		return 0, 0
	}
	// Keep all intermediate arithmetic in big.Int.  The stored value is
	// int64, but old*3 overflows for perfectly valid wallet values near the
	// int64 boundary.
	value := big.NewInt(old)
	negative := value.Sign() < 0
	if negative {
		value.Abs(value)
	}
	n := new(big.Int).Mul(value, big.NewInt(Numerator))
	d := big.NewInt(Denominator)
	q, r := new(big.Int), new(big.Int)
	q.QuoRem(n, d, r)
	if new(big.Int).Mul(r, big.NewInt(2)).Cmp(d) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	if negative {
		q.Neg(q)
	}
	oldBig := big.NewInt(old)
	delta := new(big.Int).Sub(new(big.Int).Mul(new(big.Int).Set(q), d), new(big.Int).Mul(oldBig, big.NewInt(Numerator)))
	return q.Int64(), delta.Int64()
}

func tableExists(db *gorm.DB, table string) bool {
	return db != nil && validIdentifier.MatchString(table) && db.Migrator().HasTable(table)
}

func InventoryDB(ctx context.Context, db *gorm.DB, migrationID, receiptTable string) (Inventory, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if db == nil || strings.TrimSpace(migrationID) == "" {
		return Inventory{}, ErrInvalidPlan
	}
	if receiptTable == "" {
		receiptTable = DefaultReceiptTable
	}
	if !validIdentifier.MatchString(receiptTable) {
		return Inventory{}, fmt.Errorf("invalid receipt table")
	}
	result := Inventory{MigrationID: migrationID, GeneratedAt: time.Now().Unix(), ReceiptReady: receiptTableReady(db, receiptTable)}
	for _, spec := range specs {
		snapshot, err := snapshotTable(ctx, db, spec.table)
		if err != nil {
			return Inventory{}, err
		}
		result.Tables = append(result.Tables, snapshot)
		if spec.required && !snapshot.Exists {
			result.MissingCore = append(result.MissingCore, spec.table)
		}
	}
	for _, table := range append(append([]string{}, historyTables...), unsupportedTables...) {
		if !tableExists(db, table) {
			continue
		}
		snapshot, err := snapshotTable(ctx, db, table)
		if err != nil {
			return Inventory{}, err
		}
		result.Tables = append(result.Tables, snapshot)
		if snapshot.Rows > 0 {
			blocker, err := historyBlocker(db.WithContext(ctx), table)
			if err != nil {
				return Inventory{}, err
			}
			if blocker != "" {
				result.Unsupported = append(result.Unsupported, blocker)
			}
		}
	}
	sort.Strings(result.Unsupported)
	return result, nil
}

func snapshotTable(ctx context.Context, db *gorm.DB, table string) (TableSnapshot, error) {
	s := TableSnapshot{Table: table, Exists: tableExists(db, table)}
	if !s.Exists {
		return s, nil
	}
	if err := db.WithContext(ctx).Table(table).Count(&s.Rows).Error; err != nil {
		return s, err
	}
	columns, err := db.Migrator().ColumnTypes(table)
	if err != nil {
		return s, err
	}
	hasID := false
	hasQuota := false
	for _, column := range columns {
		if strings.EqualFold(column.Name(), "id") {
			hasID = true
		}
		if strings.EqualFold(column.Name(), "quota") {
			hasQuota = true
		}
	}
	if hasID {
		var max struct{ Value *int64 }
		if err := db.WithContext(ctx).Table(table).Select("MAX(id) AS value").Scan(&max).Error; err != nil {
			return s, err
		}
		if max.Value != nil {
			s.MaxID = *max.Value
		}
	}
	if hasQuota && !unsupportedTableSet[table] {
		var values []*int64
		if err := db.WithContext(ctx).Table(table).Select("quota").Find(&values).Error; err != nil {
			return s, err
		}
		total := new(big.Int)
		for _, value := range values {
			if value != nil {
				total.Add(total, big.NewInt(*value))
			}
		}
		if !total.IsInt64() {
			return s, fmt.Errorf("%s quota sum overflows int64; refusing inventory", table)
		}
		s.Sum = total.Int64()
	}
	return s, nil
}

func BuildPlan(ctx context.Context, db *gorm.DB, migrationID, receiptTable string) (Plan, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if receiptTable == "" {
		receiptTable = DefaultReceiptTable
	}
	inv, err := InventoryDB(ctx, db, migrationID, receiptTable)
	if err != nil {
		return Plan{}, err
	}
	p := Plan{Version: PlanVersion, MigrationID: migrationID, Numerator: Numerator, Denominator: Denominator, Rounding: "half-away-from-zero", ReceiptTable: receiptTable, Source: inv.Tables, GeneratedAt: time.Now().Unix()}
	p.Blockers = append(p.Blockers, inv.Unsupported...)
	p.Blockers = append(p.Blockers, inv.MissingCore...)
	if !inv.ReceiptReady {
		p.Blockers = append(p.Blockers, "receipt table is missing: "+receiptTable)
	}
	for _, spec := range specs {
		if !tableExists(db, spec.table) {
			continue
		}
		if err := requireColumns(db, spec); err != nil {
			p.Blockers = append(p.Blockers, err.Error())
			continue
		}
		var rows []map[string]any
		query := db.WithContext(ctx).Table(spec.table).Select(strings.Join(spec.cols, ","))
		if spec.table == "redemptions" {
			if !db.Migrator().HasColumn(spec.table, "status") {
				p.Blockers = append(p.Blockers, "redemptions is missing status")
				continue
			}
			var unknown int64
			if err := db.Table(spec.table).Where("status IS NULL OR status NOT IN ?", []int{common.RedemptionCodeStatusEnabled, common.RedemptionCodeStatusDisabled, common.RedemptionCodeStatusUsed}).Count(&unknown).Error; err != nil {
				return Plan{}, err
			}
			if unknown > 0 {
				p.Blockers = append(p.Blockers, "redemptions has unknown status")
				continue
			}
			query = query.Where("status IN ?", []int{common.RedemptionCodeStatusEnabled, common.RedemptionCodeStatusDisabled})
		}
		if err := query.Find(&rows).Error; err != nil {
			return Plan{}, err
		}
		for _, row := range rows {
			id, err := asInt64(row["id"])
			if err != nil || id <= 0 {
				return Plan{}, fmt.Errorf("%s has invalid id", spec.table)
			}
			before := make(map[string]string)
			after := make(map[string]string)
			for _, col := range spec.cols[1:] {
				if col == "setting" {
					value := asString(row[col])
					converted, changed, err := convertSetting(value)
					if err != nil {
						return Plan{}, fmt.Errorf("users.%d.setting: %w", id, err)
					}
					before[col] = value
					after[col] = converted
					if !changed {
						after[col] = value
					}
					continue
				}
				value, err := asInt64(row[col])
				if err != nil {
					return Plan{}, fmt.Errorf("%s.%d.%s: %w", spec.table, id, col, err)
				}
				newValue, _ := Convert(value)
				if spec.finiteZero[col] && value > 0 && newValue == 0 {
					p.Blockers = append(p.Blockers, fmt.Sprintf("%s.%d.%s would round to finite zero", spec.table, id, col))
					continue
				}
				before[col] = strconv.FormatInt(value, 10)
				after[col] = strconv.FormatInt(newValue, 10)
			}
			p.Entries = append(p.Entries, Entry{Table: spec.table, ID: id, Before: before, After: after})
		}
	}
	sort.Slice(p.Entries, func(i, j int) bool {
		if p.Entries[i].Table == p.Entries[j].Table {
			return p.Entries[i].ID < p.Entries[j].ID
		}
		return p.Entries[i].Table < p.Entries[j].Table
	})
	p.PlanHash = hashPlan(p)
	return p, nil
}

func receiptTableReady(db *gorm.DB, table string) bool {
	if !tableExists(db, table) {
		return false
	}
	cols, err := db.Migrator().ColumnTypes(table)
	if err != nil {
		return false
	}
	want := map[string]bool{"migration_id": true, "plan_hash": true, "state": true, "before_image": true, "after_image": true, "created_at": true, "updated_at": true, "pricing_plan_hash": true, "options_before": true, "options_after": true}
	for _, col := range cols {
		delete(want, strings.ToLower(col.Name()))
	}
	return len(want) == 0
}

func requireColumns(db *gorm.DB, spec columnSpec) error {
	cols, err := db.Migrator().ColumnTypes(spec.table)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, c := range cols {
		seen[strings.ToLower(c.Name())] = true
	}
	for _, name := range spec.cols {
		if !seen[strings.ToLower(name)] {
			return fmt.Errorf("%s is missing column %s", spec.table, name)
		}
	}
	return nil
}

func convertSetting(raw string) (string, bool, error) {
	if strings.TrimSpace(raw) == "" {
		return raw, false, nil
	}
	var object map[string]any
	if err := common.Unmarshal([]byte(raw), &object); err != nil {
		return raw, false, err
	}
	value, ok := object["quota_warning_threshold"]
	if !ok {
		return raw, false, nil
	}
	old, ok := settingThresholdInt64(value)
	if !ok {
		return raw, false, fmt.Errorf("quota_warning_threshold is not an integer")
	}
	newValue, _ := Convert(old)
	object["quota_warning_threshold"] = newValue
	encoded, err := common.Marshal(object)
	return string(encoded), true, err
}

// quota_warning_threshold is stored as float64 in UserSetting, while the
// runtime comparison truncates it to int64.  Mirror that behavior before the
// currency conversion instead of rejecting a valid fractional setting.
func settingThresholdInt64(value any) (int64, bool) {
	if number, ok := value.(float64); ok {
		const positiveOverflow = 9223372036854775808.0
		if math.IsNaN(number) || math.IsInf(number, 0) || number >= positiveOverflow || number < -positiveOverflow {
			return 0, false
		}
		return int64(number), true
	}
	return numberAsInt64(value)
}

func numberAsInt64(value any) (int64, bool) {
	switch v := value.(type) {
	case float64:
		// float64 rounds MaxInt64 to 2^63. Reject that rounded positive value
		// before int64 conversion would wrap it to MinInt64.
		const positiveOverflow = 9223372036854775808.0
		if v != math.Trunc(v) || v >= positiveOverflow || v < -positiveOverflow {
			return 0, false
		}
		return int64(v), true
	case int64:
		return v, true
	case int:
		return int64(v), true
	case string:
		n, e := strconv.ParseInt(v, 10, 64)
		return n, e == nil
	default:
		return 0, false
	}
}
func asInt64(value any) (int64, error) {
	if n, ok := numberAsInt64(value); ok {
		return n, nil
	}
	if b, ok := value.([]byte); ok {
		n, e := strconv.ParseInt(string(b), 10, 64)
		return n, e
	}
	return 0, fmt.Errorf("not an integer")
}
func asString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case []byte:
		return string(v)
	default:
		return fmt.Sprint(v)
	}
}

func hashPlan(p Plan) string {
	p.PlanHash = ""
	data, _ := common.Marshal(p)
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:])
}
func Marshal(v any) ([]byte, error)      { return common.Marshal(v) }
func Unmarshal(data []byte, v any) error { return common.Unmarshal(data, v) }

func Apply(ctx context.Context, db *gorm.DB, p Plan, contract MaintenanceContract, pricing ...PricingPlan) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := contract.Validate(p.MigrationID); err != nil {
		return err
	}
	if p.Version != PlanVersion || p.Numerator != Numerator || p.Denominator != Denominator || p.PlanHash == "" || hashPlan(p) != p.PlanHash {
		return ErrInvalidPlan
	}
	if len(p.Blockers) > 0 {
		return fmt.Errorf("%w: %s", ErrUnsupportedData, strings.Join(p.Blockers, "; "))
	}
	if !receiptTableReady(db, p.ReceiptTable) {
		return ErrReceiptMissing
	}
	if len(pricing) > 1 {
		return ErrInvalidPlan
	}
	var pricePlan PricingPlan
	if len(pricing) == 1 {
		pricePlan = pricing[0]
		if pricePlan.MigrationID != p.MigrationID {
			return ErrInvalidPlan
		}
		if contract.LogDatabaseMode != "main" {
			return fmt.Errorf("%w: independent or unknown LOG_DB requires a separate bound inventory", ErrUnsupportedData)
		}
		if err := ValidatePricingApplyGuard(pricePlan, PricingApplyEvidence{PlanHash: pricePlan.PlanHash, MaintenanceLocked: contract.MaintenanceLocked, WritesStopped: contract.WritesStopped, BackupReadable: contract.BackupReadable, Observed: pricePlan.Source}); err != nil {
			return err
		}
	} else {
		if tableExists(db, "options") {
			return fmt.Errorf("%w: application databases require atomic pricing and epoch", ErrUnsupportedData)
		}
		// The old API remains useful for empty offline fixtures. Real historical
		// data may only cross epochs with the atomic pricing/cutover operation.
		for _, source := range p.Source {
			if unsupportedTableSet[source.Table] && source.Rows > 0 {
				return fmt.Errorf("%w: retained history requires atomic pricing and epoch", ErrUnsupportedData)
			}
		}
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing Receipt
		q := tx.Table(p.ReceiptTable).Where("migration_id = ?", p.MigrationID).Take(&existing)
		if q.Error == nil {
			state := existing.State
			if state == "applied" {
				if existing.PlanHash != p.PlanHash || existing.PricingPlanHash != pricePlan.PlanHash {
					return ErrPlanDrift
				}
				// Idempotent does not mean blind: a repeated invocation must
				// still prove that the rows are at the recorded after-image.
				var afterEntries []Entry
				if err := common.Unmarshal([]byte(existing.AfterImage), &afterEntries); err != nil {
					return err
				}
				if err := checkEntries(tx, afterEntries, true); err != nil {
					return err
				}
				changes, err := receiptOptionChanges(existing)
				if err != nil {
					return err
				}
				return checkOptions(tx, changes, true)
			}
			return fmt.Errorf("migration receipt already exists in state %s", state)
		}
		if !errors.Is(q.Error, gorm.ErrRecordNotFound) {
			return q.Error
		}
		if err := checkSourceSnapshots(ctx, tx, p.Source); err != nil {
			return err
		}
		fresh, err := BuildPlan(ctx, tx, p.MigrationID, p.ReceiptTable)
		if err != nil {
			return err
		}
		fresh.GeneratedAt = p.GeneratedAt
		if hashPlan(fresh) != p.PlanHash {
			return fmt.Errorf("%w: regenerated wallet plan differs", ErrPlanDrift)
		}
		if err := checkEntries(tx, p.Entries, false); err != nil {
			return err
		}
		var options []OptionChange
		now := time.Now().Unix()
		if len(pricing) == 1 {
			if err := verifyOptionCoverage(tx, pricePlan); err != nil {
				return err
			}
			legacy := map[string]int64{}
			for _, table := range append(append([]string{}, historyTables...), "redemptions") {
				legacy[table] = 0
			}
			for _, source := range p.Source {
				legacy[source.Table] = source.MaxID
			}
			legacyUsedRedemptionIDs := make([]int64, 0)
			if tx.Migrator().HasTable("redemptions") {
				if err := tx.Table("redemptions").Where("status = ?", common.RedemptionCodeStatusUsed).Order("id").Pluck("id", &legacyUsedRedemptionIDs).Error; err != nil {
					return err
				}
			}
			cutover, err := common.Marshal(common.CreditEpochCutover{Version: 1, MigrationID: p.MigrationID, SourceEpoch: common.CreditEpochLegacy, TargetEpoch: common.CreditEpochUSD, Numerator: Numerator, Denominator: Denominator, PlanHash: p.PlanHash, PricingPlanHash: pricePlan.PlanHash, LegacyMaxIDs: legacy, AppliedAt: now, LegacyTopUpPolicy: "hold", LegacyUsedRedemptionIDs: legacyUsedRedemptionIDs})
			if err != nil {
				return err
			}
			options = append(append([]OptionChange{}, pricePlan.Options...), OptionChange{Key: common.CreditEpochOption, Before: nil, After: string(cutover)})
		}
		if err := updateEntries(tx, p.Entries, false); err != nil {
			return err
		}
		if err := writeOptions(tx, options, false); err != nil {
			return err
		}
		before, _ := common.Marshal(imageEntries(p.Entries, false))
		after, _ := common.Marshal(imageEntries(p.Entries, true))
		optionsBefore, optionsAfter, err := optionReceiptImages(options)
		if err != nil {
			return err
		}
		return tx.Table(p.ReceiptTable).Create(&Receipt{MigrationID: p.MigrationID, PlanHash: p.PlanHash, PricingPlanHash: pricePlan.PlanHash, State: "applied", BeforeImage: string(before), AfterImage: string(after), OptionsBefore: optionsBefore, OptionsAfter: optionsAfter, CreatedAt: now, UpdatedAt: now}).Error
	})
}

func imageEntries(entries []Entry, after bool) []Entry {
	images := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		image := Entry{Table: entry.Table, ID: entry.ID}
		if after {
			image.After = entry.After
		} else {
			image.Before = entry.Before
		}
		images = append(images, image)
	}
	return images
}

func checkSourceSnapshots(ctx context.Context, db *gorm.DB, expected []TableSnapshot) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for _, want := range expected {
		got, err := snapshotTable(ctx, db, want.Table)
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("%w: table %s changed (expected rows=%d max_id=%d sum=%d, got rows=%d max_id=%d sum=%d)", ErrPlanDrift, want.Table, want.Rows, want.MaxID, want.Sum, got.Rows, got.MaxID, got.Sum)
		}
	}
	return nil
}

func Verify(ctx context.Context, db *gorm.DB, p Plan) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if db != nil {
		db = db.WithContext(ctx)
	}
	if p.Version != PlanVersion || p.Numerator != Numerator || p.Denominator != Denominator || p.PlanHash == "" || hashPlan(p) != p.PlanHash {
		return ErrInvalidPlan
	}
	if err := checkEntries(db, p.Entries, true); err != nil {
		return err
	}
	var receipt Receipt
	if err := db.Table(p.ReceiptTable).Where("migration_id = ?", p.MigrationID).Take(&receipt).Error; err != nil {
		return err
	}
	if receipt.State != "applied" || receipt.PlanHash != p.PlanHash {
		return ErrPlanDrift
	}
	options, err := receiptOptionChanges(receipt)
	if err != nil {
		return err
	}
	return checkOptions(db, options, true)
}

func Restore(ctx context.Context, db *gorm.DB, migrationID, receiptTable string, contract MaintenanceContract) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := contract.Validate(migrationID); err != nil {
		return err
	}
	if receiptTable == "" {
		receiptTable = DefaultReceiptTable
	}
	if !receiptTableReady(db, receiptTable) {
		return ErrReceiptMissing
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var raw Receipt
		if err := tx.Table(receiptTable).Where("migration_id = ?", migrationID).Take(&raw).Error; err != nil {
			return err
		}
		if raw.PricingPlanHash != "" && (!contract.MaintenanceLocked || !contract.WritesStopped || !contract.BackupReadable || contract.LogDatabaseMode != "main") {
			return ErrMaintenanceRequired
		}
		if raw.State != "applied" && raw.State != "restored" {
			return fmt.Errorf("receipt is not applied")
		}
		var entries []Entry
		if err := common.Unmarshal([]byte(raw.BeforeImage), &entries); err != nil {
			return err
		}
		if raw.State == "restored" {
			if err := checkEntries(tx, entries, false); err != nil {
				return err
			}
			options, err := receiptOptionChanges(raw)
			if err != nil {
				return err
			}
			return checkOptions(tx, options, false)
		}
		var afterEntries []Entry
		if err := common.Unmarshal([]byte(raw.AfterImage), &afterEntries); err != nil {
			return err
		}
		if err := checkEntries(tx, afterEntries, true); err != nil {
			return err
		}
		options, err := receiptOptionChanges(raw)
		if err != nil {
			return err
		}
		if err := checkOptions(tx, options, true); err != nil {
			return err
		}
		if err := updateEntries(tx, entries, true); err != nil {
			return err
		}
		if err := writeOptions(tx, options, true); err != nil {
			return err
		}
		return tx.Table(receiptTable).Where("migration_id = ?", migrationID).Updates(map[string]any{"state": "restored", "updated_at": time.Now().Unix()}).Error
	})
}

func checkEntries(db *gorm.DB, entries []Entry, expectAfter bool) error {
	for _, e := range entries {
		row := map[string]any{}
		if err := db.Table(e.Table).Where("id = ?", e.ID).Take(&row).Error; err != nil {
			return err
		}
		expected := e.Before
		if expectAfter {
			expected = e.After
		}
		for col, want := range expected {
			got := row[col]
			if col == "setting" {
				if asString(got) != asString(want) {
					return fmt.Errorf("%w: %s/%d.%s", ErrPlanDrift, e.Table, e.ID, col)
				}
			} else {
				a, err := asInt64(got)
				if err != nil {
					return err
				}
				b, err := asInt64(want)
				if err != nil {
					return err
				}
				if a != b {
					return fmt.Errorf("%w: %s/%d.%s", ErrPlanDrift, e.Table, e.ID, col)
				}
			}
		}
	}
	return nil
}
func updateEntries(db *gorm.DB, entries []Entry, restore bool) error {
	for _, e := range entries {
		values := e.After
		if restore {
			values = e.Before
		}
		if len(values) == 0 {
			continue
		}
		updates := make(map[string]any, len(values))
		for column, value := range values {
			if column == "setting" {
				updates[column] = value
				continue
			}
			number, err := asInt64(value)
			if err != nil {
				return err
			}
			updates[column] = number
		}
		if err := db.Table(e.Table).Where("id = ?", e.ID).Updates(updates).Error; err != nil {
			return err
		}
	}
	return nil
}
