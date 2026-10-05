package creditmigration

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// historyBlocker preserves raw historical amounts, but never treats a terminal
// provider status alone as proof that wallet settlement finished.
func historyBlocker(db *gorm.DB, table string) (string, error) {
	var columns []string
	var safeStatuses []string
	switch table {
	case "logs", "quota_data", "checkins", "kkai_internal_balance_adjustments":
		return "", nil
	case "top_ups":
		// Old pending orders are held by the cutover policy. Preserve even
		// malformed legacy payment facts; never repair, expire or credit them.
		columns = []string{"status"}
		safeStatuses = []string{common.TopUpStatusSuccess, common.TopUpStatusFailed, common.TopUpStatusExpired, common.TopUpStatusPending}
	case "subscription_orders":
		columns = []string{"status"}
		safeStatuses = []string{common.TopUpStatusSuccess, common.TopUpStatusFailed, common.TopUpStatusExpired}
	case "kkai_outbox":
		columns = []string{"status"}
		// Dead events are retained on hold; the runtime only dispatches pending.
		safeStatuses = []string{"delivered", "dead"}
	case "subscription_pre_consume_records":
		// "consumed" may be either active or historical. A request lifecycle
		// reconciliation is required before accepting those rows.
		columns = []string{"status"}
		safeStatuses = []string{"refunded"}
	case "tasks":
		columns = []string{"status", "private_data"}
	case "kkai_image_generations":
		columns = []string{"status", "billing_state"}
	default:
		return fmt.Sprintf("%s has data without a verified settlement/ledger adapter", table), nil
	}
	for _, column := range columns {
		if !db.Migrator().HasColumn(table, column) {
			return fmt.Sprintf("%s is missing settlement column %s", table, column), nil
		}
	}
	if len(safeStatuses) != 0 {
		var count int64
		if err := db.Table(table).Where("status IS NULL OR status NOT IN ?", safeStatuses).Count(&count).Error; err != nil {
			return "", err
		}
		if count != 0 {
			return fmt.Sprintf("%s has %d unfinished or unknown records", table, count), nil
		}
		return "", nil
	}
	if table == "kkai_image_generations" {
		var count int64
		err := db.Table(table).Where("status IS NULL OR status NOT IN ? OR billing_state IS NULL OR billing_state NOT IN ?", []string{"succeeded", "partial", "failed", "archive_failed"}, []string{"settled", "refunded"}).Count(&count).Error
		if err != nil {
			return "", err
		}
		if count != 0 {
			return fmt.Sprintf("%s has %d unsettled or unknown records", table, count), nil
		}
		return "", nil
	}
	var rows []struct {
		Status      string
		PrivateData string
	}
	if err := db.Table(table).Select("status, private_data").Find(&rows).Error; err != nil {
		return "", err
	}
	for _, row := range rows {
		var settlement struct {
			BillingState       string `json:"billing_state"`
			AccountingState    string `json:"accounting_state"`
			AccountingRequired bool   `json:"accounting_required"`
		}
		if common.UnmarshalJsonStr(row.PrivateData, &settlement) != nil {
			return "tasks has unfinished or unproven billing/accounting settlement", nil
		}
		terminal := row.Status == "SUCCESS" || row.Status == "FAILURE"
		// Before durable settlement existed, terminal tasks had no settlement
		// fields. Preserve those records; runtime cutover guards hold all old
		// task funding, so an explicit retry cannot apply their raw amounts.
		legacyTerminal := terminal && settlement.BillingState == "" &&
			settlement.AccountingState == "" && !settlement.AccountingRequired
		if legacyTerminal {
			continue
		}
		if (!terminal && row.Status != "UNKNOWN") ||
			(settlement.BillingState != "completed" && settlement.BillingState != "refunded") ||
			(settlement.AccountingRequired && settlement.AccountingState != "completed") ||
			(settlement.AccountingState != "" && settlement.AccountingState != "completed") {
			return "tasks has unfinished or unproven billing/accounting settlement", nil
		}
	}
	return "", nil
}
