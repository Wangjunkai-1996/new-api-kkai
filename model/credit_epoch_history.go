package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// creditEpochQuotaExpression normalizes each raw historical row before SUM.
// Quotient/remainder arithmetic avoids overflowing quota*3, including MinInt64.
func creditEpochQuotaExpression(db *gorm.DB, table string) (string, error) {
	column := "quota"
	switch table {
	case "logs", "quota_data":
	case "checkins":
		column = "quota_awarded"
	default:
		return "", common.ErrCreditEpochInvalid
	}
	epoch, err := common.CurrentCreditEpoch()
	if err != nil {
		return "", err
	}
	if epoch == nil {
		return column, nil
	}
	cutoff, exists := epoch.LegacyMaxIDs[table]
	if !exists || db == nil {
		return "", common.ErrCreditEpochInvalid
	}
	// The caller supplies the actual main/log handle so independent log
	// databases and isolated test handles use their own SQL dialect.
	divide := "/"
	switch db.Dialector.Name() {
	case "mysql":
		divide = "DIV"
	case "postgres", "sqlite":
	default:
		return "", fmt.Errorf("%w: historical quota dialect %s", common.ErrCreditEpochInvalid, db.Dialector.Name())
	}
	q := "COALESCE(" + column + ", 0)"
	return fmt.Sprintf("CASE WHEN id <= %d THEN ((%s %s 40) * 3 + CASE WHEN %s < 0 THEN - (((-(%s %% 40)) * 3 + 20) %s 40) ELSE (((%s %% 40) * 3 + 20) %s 40) END) ELSE %s END", cutoff, q, divide, q, q, divide, q, divide, q), nil
}
