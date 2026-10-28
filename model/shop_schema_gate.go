package model

import (
	"errors"
	"gorm.io/gorm"
	"strings"
	"sync"
)

var shopSchemaChecker struct {
	sync.RWMutex
	check func(*gorm.DB) error
}

// RegisterShopSchemaChecker avoids a migration package/model dependency cycle.
// Production installs the canonical checker at service initialization.
func RegisterShopSchemaChecker(check func(*gorm.DB) error) {
	shopSchemaChecker.Lock()
	defer shopSchemaChecker.Unlock()
	shopSchemaChecker.check = check
}

func shopDeletionSchemaReady(tx *gorm.DB) (bool, error) {
	tables, err := tx.Migrator().GetTables()
	if err != nil {
		return false, err
	}
	expected, ledger := false, false
	for _, table := range tables {
		expected = expected || strings.HasPrefix(table, "shop_")
		ledger = ledger || table == "kkai_schema_migrations"
	}
	if ledger {
		var n int64
		if err = tx.Table("kkai_schema_migrations").Where("version >= ?", 19).Count(&n).Error; err != nil {
			return false, err
		}
		expected = expected || n > 0
	}
	if !expected {
		return false, nil
	}
	shopSchemaChecker.RLock()
	check := shopSchemaChecker.check
	shopSchemaChecker.RUnlock()
	if check == nil {
		return false, errors.New("shop schema verification is unavailable")
	}
	if err = check(tx); err != nil {
		return false, err
	}
	return true, nil
}
