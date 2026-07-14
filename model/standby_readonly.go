package model

import (
	"errors"
	"strings"

	"gorm.io/gorm"
)

var ErrStandbyReadOnly = errors.New("standby-readonly node rejected a database write")

const standbyReadOnlyCallbackName = "kkai:standby-readonly"

func EnableStandbyReadOnlyGuard(db *gorm.DB) error {
	if db == nil {
		return ErrStandbyReadOnly
	}
	rejectWrite := func(tx *gorm.DB) {
		tx.AddError(ErrStandbyReadOnly)
	}
	rejectUnsafeRaw := func(tx *gorm.DB) {
		if !standbyRawSQLIsReadOnly(tx.Statement.SQL.String()) {
			tx.AddError(ErrStandbyReadOnly)
		}
	}
	return errors.Join(
		db.Callback().Create().Before("gorm:create").Register(standbyReadOnlyCallbackName, rejectWrite),
		db.Callback().Update().Before("gorm:update").Register(standbyReadOnlyCallbackName, rejectWrite),
		db.Callback().Delete().Before("gorm:delete").Register(standbyReadOnlyCallbackName, rejectWrite),
		db.Callback().Raw().Before("gorm:raw").Register(standbyReadOnlyCallbackName, rejectUnsafeRaw),
	)
}

func standbyRawSQLIsReadOnly(statement string) bool {
	normalized := strings.TrimSpace(statement)
	if normalized == "" {
		return false
	}
	if strings.HasSuffix(normalized, ";") {
		normalized = strings.TrimSpace(strings.TrimSuffix(normalized, ";"))
	}
	if strings.Contains(normalized, ";") {
		return false
	}
	upper := strings.ToUpper(normalized)
	for _, sideEffect := range []string{
		"ADVISORY_LOCK",
		"GET_LOCK(",
		"RELEASE_LOCK(",
		"NEXTVAL(",
		"SETVAL(",
		"SET_CONFIG(",
		" FOR UPDATE",
		" FOR NO KEY UPDATE",
		" FOR SHARE",
		" FOR KEY SHARE",
		" LOCK IN SHARE MODE",
		" INTO OUTFILE",
		" INTO DUMPFILE",
	} {
		if strings.Contains(upper, sideEffect) {
			return false
		}
	}
	if strings.HasPrefix(upper, "PRAGMA ") {
		pragma := strings.TrimSpace(upper[len("PRAGMA "):])
		if strings.Contains(pragma, "=") {
			return false
		}
		for _, prefix := range []string{
			"TABLE_INFO(",
			"TABLE_XINFO(",
			"INDEX_LIST(",
			"INDEX_INFO(",
			"INDEX_XINFO(",
			"FOREIGN_KEY_LIST(",
			"DATABASE_LIST",
			"USER_VERSION",
			"SCHEMA_VERSION",
		} {
			if strings.HasPrefix(pragma, prefix) {
				return true
			}
		}
		return false
	}
	for _, prefix := range []string{"SELECT ", "SHOW ", "EXPLAIN ", "DESCRIBE "} {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}
