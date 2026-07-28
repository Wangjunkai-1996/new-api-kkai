package model

import (
	"context"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// lockForUpdate makes the next query emit SELECT ... FOR UPDATE so the matched
// rows stay locked until the surrounding transaction ends.
//
// GORM v2 silently ignores the legacy `Set("gorm:query_option", "FOR UPDATE")`
// from GORM v1, so that form does not lock anything. Always use this helper
// instead.
//
// SQLite has no FOR UPDATE syntax (the clause would be a syntax error), so it
// is skipped there; SQLite's single-writer model makes one of two conflicting
// transactions fail instead of both committing.
func lockForUpdate(tx *gorm.DB) *gorm.DB {
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		return tx
	}
	return tx.Clauses(clause.Locking{Strength: "UPDATE"})
}

// LockUserForTokenCreation returns the current owner state under the creation lock.
func LockUserForTokenCreation(ctx context.Context, tx *gorm.DB, userID int) (*User, error) {
	var user User
	err := lockForUpdate(tx.WithContext(ctx)).
		Select("id", "group", "status").
		First(&user, "id = ?", userID).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}
