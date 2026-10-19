package kkaimigrate

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

func populateTenantOnlineIdentity(ctx context.Context, db *gorm.DB, status *TenantOnlineExpansionStatus) error {
	name, schema, err := TenantOnlineDatabaseIdentity(ctx, db)
	if err != nil {
		return err
	}
	status.DatabaseName, status.DatabaseSchema = name, schema
	status.SourceDigest = migrationSetDigest(db.Dialector.Name(), planItemsForDialect(db.Dialector.Name(), MembershipSchemaVersion))
	return nil
}

// TenantOnlineDatabaseIdentity contains no connection secrets. A production
// controller additionally binds these names to its inspected writer endpoint.
func TenantOnlineDatabaseIdentity(ctx context.Context, db *gorm.DB) (string, string, error) {
	if db == nil {
		return "", "", ErrSchemaNotReady
	}
	var identity struct{ Name, Schema string }
	switch db.Dialector.Name() {
	case DialectPostgres:
		if err := db.WithContext(ctx).Raw("SELECT current_database() AS name,current_schema() AS schema").Scan(&identity).Error; err != nil {
			return "", "", err
		}
	case DialectMySQL:
		if err := db.WithContext(ctx).Raw("SELECT DATABASE() AS name").Scan(&identity).Error; err != nil {
			return "", "", err
		}
		identity.Schema = identity.Name
	case DialectSQLite:
		var rows []struct{ Name, File string }
		if err := db.WithContext(ctx).Raw("PRAGMA database_list").Scan(&rows).Error; err != nil {
			return "", "", err
		}
		for _, row := range rows {
			if row.Name == "main" {
				identity.Name, identity.Schema = row.File, "main"
			}
		}
	default:
		return "", "", ErrUnsupportedDialect
	}
	if identity.Schema == "" {
		return "", "", fmt.Errorf("%w: database schema identity is empty", ErrSchemaNotReady)
	}
	return identity.Name, identity.Schema, nil
}
