package service

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func lockVideoRowsForUpdate(query *gorm.DB) *gorm.DB {
	if query == nil || query.Dialector == nil || query.Dialector.Name() == "sqlite" {
		return query
	}
	return query.Clauses(clause.Locking{Strength: "UPDATE"})
}
