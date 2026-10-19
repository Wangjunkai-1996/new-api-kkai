package kkaimigrate

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

func tenantOnlineSupportedDatabase(db *gorm.DB, offline bool) error {
	if offline {
		_, err := dialectName(db)
		return err
	}
	switch db.Dialector.Name() {
	case DialectSQLite:
		return nil // Explicit local fallback; no online-concurrency claim.
	case DialectPostgres:
		var version int
		if err := db.Raw("SELECT current_setting('server_version_num')::int").Scan(&version).Error; err != nil {
			return err
		}
		if version < 120000 {
			return fmt.Errorf("%w: tenant online expansion requires PostgreSQL >=12 metadata-fast defaults", ErrUnsafeMigration)
		}
		return nil
	default:
		return fmt.Errorf("%w: tenant online expansion supports PostgreSQL >=12 or the SQLite local fallback; other supported databases require isolated or write-stopped library Options{Offline:true}, which has no production CLI", ErrUnsupportedDialect)
	}
}

func withTenantOnlineLock(ctx context.Context, db *gorm.DB, offline bool, fn func(*gorm.DB) error) (result error) {
	if db == nil {
		return ErrSchemaNotReady
	}
	if !migrationMu.TryLock() {
		return fmt.Errorf("%w: another migration is running", ErrUnsafeMigration)
	}
	defer migrationMu.Unlock()
	if err := tenantOnlineSupportedDatabase(db.WithContext(ctx), offline); err != nil {
		return err
	}
	if db.Dialector.Name() == DialectSQLite {
		return fn(db.WithContext(ctx))
	}
	if db.Dialector.Name() == DialectMySQL {
		// The historical lock pins one connection across MySQL auto-commit DDL.
		// migrationMu is already held, so acquire only the database lock here.
		sqlDB, err := db.DB()
		if err != nil {
			return err
		}
		conn, err := sqlDB.Conn(ctx)
		if err != nil {
			return err
		}
		defer conn.Close()
		locked := db.Session(&gorm.Session{NewDB: true, Context: ctx})
		locked.Statement.ConnPool = conn
		var acquired int
		if err := locked.Raw("SELECT GET_LOCK(?,0)", "kkai_schema_migrations").Scan(&acquired).Error; err != nil {
			return err
		}
		if acquired != 1 {
			return fmt.Errorf("%w: another migration owns the schema lock", ErrUnsafeMigration)
		}
		defer func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var released int
			err := locked.WithContext(cleanupCtx).Raw("SELECT RELEASE_LOCK(?)", "kkai_schema_migrations").Scan(&released).Error
			if err != nil || released != 1 {
				_ = conn.Raw(func(any) error { return driver.ErrBadConn })
				result = errors.Join(result, err, fmt.Errorf("failed to release offline migration lock"))
			}
		}()
		return fn(locked)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	locked := db.Session(&gorm.Session{NewDB: true, Context: ctx})
	locked.Statement.ConnPool = conn
	var settings struct{ LockTimeout, StatementTimeout string }
	if err := locked.Raw("SELECT current_setting('lock_timeout') AS lock_timeout,current_setting('statement_timeout') AS statement_timeout").Scan(&settings).Error; err != nil {
		return err
	}
	var acquired bool
	if err := locked.Raw("SELECT pg_try_advisory_lock(hashtext(?))", "kkai_schema_migrations").Scan(&acquired).Error; err != nil {
		return err
	}
	if !acquired {
		return fmt.Errorf("%w: another migration owns the schema advisory lock", ErrUnsafeMigration)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cleanup := locked.WithContext(cleanupCtx)
		resetErr := cleanup.Exec("SELECT set_config('lock_timeout', ?, false),set_config('statement_timeout', ?, false)", settings.LockTimeout, settings.StatementTimeout).Error
		unlockErr := cleanup.Exec("SELECT pg_advisory_unlock(hashtext(?))", "kkai_schema_migrations").Error
		if resetErr != nil || unlockErr != nil {
			// Do not return an altered session (or a held advisory lock) to the pool.
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		result = errors.Join(result, resetErr, unlockErr)
	}()
	if err := locked.Exec("SELECT set_config('lock_timeout','1s',false),set_config('statement_timeout','5s',false)").Error; err != nil {
		return err
	}
	return fn(locked)
}

func validateTenantOnlineLegacyProtection(db *gorm.DB) error {
	if db.Dialector.Name() == DialectPostgres {
		var protected bool
		err := db.Raw(`SELECT EXISTS (
SELECT 1 FROM pg_catalog.pg_index i
JOIN pg_catalog.pg_attribute a ON a.attrelid=i.indrelid AND a.attnum=i.indkey[0]
WHERE i.indrelid=pg_catalog.to_regclass('users') AND i.indisunique AND i.indisvalid
 AND i.indisready AND i.indimmediate AND i.indnatts=1 AND i.indpred IS NULL
 AND i.indexprs IS NULL AND a.attname='username')`).Scan(&protected).Error
		if err != nil {
			return err
		}
		if !protected {
			return fmt.Errorf("%w: missing valid global username protection", ErrSchemaNotReady)
		}
		return nil
	}
	if db.Dialector.Name() == DialectSQLite {
		var count int64
		err := db.Raw(`SELECT COUNT(*) FROM pragma_index_list('users') i
WHERE i."unique"=1 AND i.partial=0
 AND (SELECT COUNT(*) FROM pragma_index_info(i.name))=1
 AND (SELECT name FROM pragma_index_info(i.name) LIMIT 1)='username'`).Scan(&count).Error
		if err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("%w: missing full global username protection", ErrSchemaNotReady)
		}
	}
	return nil
}

func validateTenantOnlineIndex(db *gorm.DB, table, name string, expected []string, unique bool) error {
	if db.Dialector.Name() == DialectPostgres {
		var rows []struct {
			Column                      string
			Unique, Valid, Ready, Plain bool
		}
		err := db.Raw(`SELECT a.attname AS column,i.indisunique AS unique,i.indisvalid AS valid,i.indisready AS ready,
 (i.indpred IS NULL AND i.indexprs IS NULL AND i.indimmediate AND am.amname='btree'
  AND pg_catalog.pg_get_indexdef(i.indexrelid,k.position::int,false)=pg_catalog.quote_ident(a.attname)) AS plain
FROM pg_catalog.pg_index i
JOIN pg_catalog.pg_class idx ON idx.oid=i.indexrelid
JOIN pg_catalog.pg_am am ON am.oid=idx.relam
JOIN LATERAL unnest(i.indkey) WITH ORDINALITY k(attnum,position) ON true
JOIN pg_catalog.pg_attribute a ON a.attrelid=i.indrelid AND a.attnum=k.attnum
WHERE i.indrelid=pg_catalog.to_regclass(?) AND idx.relname=? ORDER BY k.position`, table, name).Scan(&rows).Error
		if err != nil {
			return err
		}
		var actual []string
		for _, row := range rows {
			if row.Unique != unique || !row.Valid || !row.Ready || !row.Plain {
				return fmt.Errorf("%w: invalid or mismatched index %s; stop and inspect, never auto-drop", ErrSchemaNotReady, name)
			}
			actual = append(actual, row.Column)
		}
		if !equalRuntimeIndexColumns(actual, expected) {
			return fmt.Errorf("%w: index %s has the wrong columns", ErrSchemaNotReady, name)
		}
		return nil
	}
	if db.Dialector.Name() == DialectSQLite {
		var indexes []struct {
			Name            string
			Unique, Partial int
		}
		if err := db.Raw(`SELECT name,"unique",partial FROM pragma_index_list(?)`, table).Scan(&indexes).Error; err != nil {
			return err
		}
		for _, index := range indexes {
			if index.Name != name {
				continue
			}
			if (index.Unique == 1) != unique || index.Partial != 0 {
				return fmt.Errorf("%w: partial or mismatched index %s", ErrSchemaNotReady, name)
			}
			var columns []struct {
				Name, Coll string
				CID        int
			}
			if err := db.Raw(`SELECT name,coll,cid FROM pragma_index_xinfo(?) WHERE "key"=1 ORDER BY seqno`, name).Scan(&columns).Error; err != nil {
				return err
			}
			var actual []string
			for _, column := range columns {
				if column.CID < 0 || ((column.Name == "tenant_id" || column.Name == "normalized_username" || column.Name == "normalized_email") && column.Coll != "BINARY") {
					return fmt.Errorf("%w: expression or collation mismatch in %s", ErrSchemaNotReady, name)
				}
				actual = append(actual, column.Name)
			}
			if !equalRuntimeIndexColumns(actual, expected) {
				return fmt.Errorf("%w: invalid columns in %s", ErrSchemaNotReady, name)
			}
			return nil
		}
		return fmt.Errorf("%w: missing tenant index %s", ErrSchemaNotReady, name)
	}
	indexes, err := db.Migrator().GetIndexes(table)
	if err != nil {
		return err
	}
	for _, index := range indexes {
		if index.Name() != name {
			continue
		}
		actualUnique, known := index.Unique()
		if !known || actualUnique != unique || !equalRuntimeIndexColumns(index.Columns(), expected) {
			return fmt.Errorf("%w: invalid index shape %s", ErrSchemaNotReady, name)
		}
		return nil
	}
	return fmt.Errorf("%w: missing tenant index %s", ErrSchemaNotReady, name)
}
