package kkaimigrate

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

type tenantOnlineStep struct {
	key, table, field, index string
	model                    any
	columns                  []string
	unique                   bool
}

func tenantOnlineSteps() []tenantOnlineStep {
	var steps []tenantOnlineStep
	for _, value := range tenantV12SnapshotModels() {
		table := value.(interface{ TableName() string }).TableName()
		steps = append(steps, tenantOnlineStep{key: "column:" + table + ".tenant_id", table: table, model: value, field: "TenantID"})
	}
	for _, entry := range []struct {
		model  any
		fields []string
	}{
		{&tenantV13User{}, []string{"NormalizedUsername", "NormalizedEmail"}},
		{&tenantV12TopUp{}, []string{"PaidMinor", "Currency"}},
	} {
		for _, field := range entry.fields {
			table := entry.model.(interface{ TableName() string }).TableName()
			steps = append(steps, tenantOnlineStep{key: "column:" + table + "." + field, table: table, model: entry.model, field: field})
		}
	}
	for _, index := range []struct {
		name    string
		columns []string
		unique  bool
	}{
		{"idx_users_tenant_username", []string{"tenant_id", "username"}, true},
		{"idx_users_tenant_email", []string{"tenant_id", "email"}, false},
		{"idx_users_tenant_normalized_username", []string{"tenant_id", "normalized_username"}, true},
		{"idx_users_tenant_normalized_email", []string{"tenant_id", "normalized_email"}, true},
	} {
		steps = append(steps, tenantOnlineStep{key: "index:" + index.name, table: "users", model: &tenantV13User{}, index: index.name, columns: index.columns, unique: index.unique})
	}
	for _, value := range tenantV12SnapshotModels() {
		table := value.(interface{ TableName() string }).TableName()
		index := "idx_" + table + "_tenant_id"
		steps = append(steps, tenantOnlineStep{key: "index:" + index, table: table, model: value, index: index, columns: []string{"tenant_id"}})
	}
	for _, value := range tenantOnlineNewModels() {
		table := value.(interface{ TableName() string }).TableName()
		steps = append(steps, tenantOnlineStep{key: "table:" + table, table: table, model: value})
	}
	return steps
}

func (step tenantOnlineStep) checksum() string {
	return tenantOnlineHash(fmt.Sprintf("%s|%s|%s|%s|%v|%t|%s", step.key, step.table, step.field, step.index, step.columns, step.unique, tenantOnlineSchemaSpec()))
}

func (step tenantOnlineStep) exists(db *gorm.DB) bool {
	if step.field != "" {
		return db.Migrator().HasColumn(step.model, step.field)
	}
	if step.index != "" {
		return db.Migrator().HasIndex(step.model, step.index)
	}
	return db.Migrator().HasTable(step.model)
}

func (step tenantOnlineStep) apply(db *gorm.DB) error {
	if step.index != "" && db.Dialector.Name() == DialectPostgres {
		kind := "INDEX"
		if step.unique {
			kind = "UNIQUE INDEX"
		}
		// This is deliberately outside a transaction. The statement is compiled
		// exclusively from the migration's fixed identifiers, never user input.
		if err := db.Exec("SELECT set_config('statement_timeout', '60s', false),set_config('lock_timeout','100ms',false)").Error; err != nil {
			return err
		}
		err := db.Exec("CREATE " + kind + " CONCURRENTLY " + step.index + " ON " + step.table + " (" + strings.Join(step.columns, ",") + ")").Error
		resetErr := db.Exec("SELECT set_config('statement_timeout', '5s', false),set_config('lock_timeout','1s',false)").Error
		if err != nil {
			return err
		}
		return resetErr
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if step.field != "" && tx.Dialector.Name() == DialectPostgres {
			if err := tx.Exec("LOCK TABLE " + step.table + " IN ACCESS EXCLUSIVE MODE NOWAIT").Error; err != nil {
				return err
			}
		}
		if step.field != "" {
			return tx.Migrator().AddColumn(step.model, step.field)
		}
		if step.index != "" {
			return tx.Migrator().CreateIndex(step.model, step.index)
		}
		return tx.Migrator().CreateTable(step.model)
	})
}

func (step tenantOnlineStep) validate(db *gorm.DB) error {
	if step.index != "" {
		return validateTenantOnlineIndex(db, step.table, step.index, step.columns, step.unique)
	}
	statement := &gorm.Statement{DB: db}
	if err := statement.Parse(step.model); err != nil {
		return err
	}
	if !db.Migrator().HasTable(step.model) {
		return fmt.Errorf("%w: missing tenant object %s", ErrSchemaNotReady, step.key)
	}
	if step.field == "" && db.Dialector.Name() == DialectPostgres {
		var primary []string
		for _, field := range statement.Schema.PrimaryFields {
			primary = append(primary, field.DBName)
		}
		if err := validateTenantOnlinePostgresPrimaryKey(db, step.table, primary); err != nil {
			return err
		}
	}
	columns, err := db.Migrator().ColumnTypes(step.model)
	if err != nil {
		return err
	}
	actual := make(map[string]gorm.ColumnType, len(columns))
	for _, column := range columns {
		actual[column.Name()] = column
	}
	for _, field := range statement.Schema.Fields {
		if step.field != "" && field.Name != step.field {
			continue
		}
		column, exists := actual[field.DBName]
		if !exists {
			return fmt.Errorf("%w: missing tenant column %s.%s", ErrSchemaNotReady, step.table, field.DBName)
		}
		if err := validateTenantOnlineColumn(db, step.table, field, column); err != nil {
			return err
		}
	}
	if step.field == "" {
		for _, index := range statement.Schema.ParseIndexes() {
			var names []string
			for _, field := range index.Fields {
				names = append(names, field.DBName)
			}
			if err := validateTenantOnlineIndex(db, step.table, index.Name, names, index.Class == "UNIQUE"); err != nil {
				return err
			}
		}
	}
	return nil
}

func tenantOnlineRootReceipt(db *gorm.DB) *tenantOnlineReceipt {
	return &tenantOnlineReceipt{Step: "source", PlanChecksum: TenantOnlinePlanChecksum(), SourceDigest: migrationSetDigest(db.Dialector.Name(), planItemsForDialect(db.Dialector.Name(), MembershipSchemaVersion)), Checksum: tenantOnlineHash("source:v11"), State: "done", CreatedAt: time.Now().Unix(), CompletedAt: time.Now().Unix()}
}

func loadTenantOnlineReceipts(db *gorm.DB) (map[string]tenantOnlineReceipt, error) {
	if !db.Migrator().HasTable(&tenantOnlineReceipt{}) {
		return nil, fmt.Errorf("%w: tenant expansion receipts are missing", ErrSchemaNotReady)
	}
	if err := (tenantOnlineStep{key: "table:kkai_tenant_online_steps", table: "kkai_tenant_online_steps", model: &tenantOnlineReceipt{}}).validate(db); err != nil {
		return nil, err
	}
	var rows []tenantOnlineReceipt
	if err := db.Find(&rows).Error; err != nil {
		return nil, err
	}
	checksums := map[string]string{"source": tenantOnlineHash("source:v11"), "contract": tenantOnlineHash("contract:" + tenantOnlineSchemaSpec())}
	for _, step := range tenantOnlineSteps() {
		checksums[step.key] = step.checksum()
	}
	root := tenantOnlineRootReceipt(db)
	result := make(map[string]tenantOnlineReceipt, len(rows))
	for _, row := range rows {
		checksum, known := checksums[row.Step]
		if !known || row.Checksum != checksum || row.PlanChecksum != root.PlanChecksum || row.SourceDigest != root.SourceDigest || (row.State != "started" && row.State != "done") {
			return nil, fmt.Errorf("%w: tenant expansion receipt %s", ErrChecksumMismatch, row.Step)
		}
		result[row.Step] = row
	}
	if row, ok := result["source"]; !ok || row.State != "done" {
		return nil, fmt.Errorf("%w: missing tenant source receipt", ErrSchemaNotReady)
	}
	return result, nil
}

func validateTenantOnlineCompleted(db *gorm.DB) error {
	receipts, err := loadTenantOnlineReceipts(db)
	if err != nil {
		return err
	}
	for _, step := range tenantOnlineSteps() {
		if receipt, ok := receipts[step.key]; !ok || receipt.State != "done" {
			return fmt.Errorf("%w: incomplete tenant expansion step %s", ErrSchemaNotReady, step.key)
		}
		if err := step.validate(db); err != nil {
			return err
		}
	}
	return nil
}

func tenantOnlineDropLegacyKeys(db *gorm.DB, keys []tenantUsernameUniqueKey) error {
	// SQLite's local fallback preserves unrelated indexes/triggers across the
	// GORM table rebuild required for named UNIQUE constraints.
	var ddl []string
	for _, key := range keys {
		if !key.Constraint {
			if err := db.Migrator().DropIndex("users", key.Name); err != nil {
				return err
			}
		}
	}
	if db.Dialector.Name() == DialectSQLite {
		if err := db.Raw("SELECT sql FROM sqlite_master WHERE type IN ('index','trigger') AND tbl_name='users' AND sql IS NOT NULL").Scan(&ddl).Error; err != nil {
			return err
		}
	}
	rebuilt := false
	for _, key := range keys {
		if key.Constraint {
			if err := db.Migrator().DropConstraint("users", key.Name); err != nil {
				return err
			}
			rebuilt = db.Dialector.Name() == DialectSQLite
		}
	}
	if rebuilt {
		for _, statement := range ddl {
			if err := db.Exec(statement).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
