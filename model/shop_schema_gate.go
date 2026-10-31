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
	expected := false
	if tx.Migrator().HasTable("kkai_schema_migrations") {
		var n int64
		if err := tx.Table("kkai_schema_migrations").Where("version >= ?", 19).Count(&n).Error; err != nil {
			return false, err
		}
		expected = n > 0
	}
	if !expected {
		tables, err := tx.Migrator().GetTables()
		if err != nil {
			return false, err
		}
		for _, table := range tables {
			if strings.HasPrefix(table, "shop_") {
				expected = true
				break
			}
		}
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
	if err := check(tx); err != nil {
		return false, err
	}
	return true, nil
}
