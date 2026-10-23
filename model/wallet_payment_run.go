package model

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// WalletRunScope is frozen in the report. Unknown original merchant identities
// must be requested explicitly; current configuration never fills old records.
type WalletRunScope struct {
	PaymentProvider string `json:"payment_provider"`
	AccountRef      string `json:"provider_account_ref"`
	Environment     string `json:"provider_environment"`
	TenantID        int64  `json:"tenant_id"`
	CreatedBefore   int64  `json:"created_before"`
	CreatedAfter    int64  `json:"created_after,omitempty"`
}
type WalletRunSelection struct {
	Scope            WalletRunScope `json:"scope"`
	BatchSize        int            `json:"batch_size"`
	QueriesPerSecond int            `json:"max_queries_per_second"`
}

func walletRunOrders(tx *gorm.DB, scope WalletRunScope) *gorm.DB {
	provider, account, environment := scope.PaymentProvider, scope.AccountRef, scope.Environment
	if provider == "unknown" {
		provider = ""
	}
	if account == "unknown" {
		account = ""
	}
	if environment == "unknown" {
		environment = ""
	}
	now := common.GetTimestamp()
	return WalletTopUpOnlyQuery(tx.Model(&TopUp{})).Where("COALESCE(payment_provider,'') = ? AND COALESCE(provider_account_ref,'') = ? AND COALESCE(provider_environment,'') = ? AND tenant_id = ? AND create_time < ? AND create_time >= ?", provider, account, environment, scope.TenantID, scope.CreatedBefore, scope.CreatedAfter).Where(WalletTopUpHistoryRetainedSQL).Where(walletTopUpActiveAttemptSQL, now-WalletTopUpAttemptTTLSeconds, now, now)
}
func ReportWalletTopUpRun(scope WalletRunScope) (maxID, total, missing int64, err error) {
	if err = validateWalletRunScope(scope); err != nil {
		return
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		q := walletRunOrders(tx, scope)
		if e := q.Session(&gorm.Session{}).Select("COALESCE(MAX(id),0)").Scan(&maxID).Error; e != nil {
			return e
		}
		if e := q.Session(&gorm.Session{}).Where("id <= ?", maxID).Count(&total).Error; e != nil {
			return e
		}
		return q.Session(&gorm.Session{}).Where("id <= ? AND (COALESCE(wallet_lifecycle_version,0) = 0 OR COALESCE(provider_account_ref,'') = '' OR COALESCE(provider_session_id,'') = '')", maxID).Count(&missing).Error
	})
	return
}
func FindWalletRunOrders(selection WalletRunSelection, cursor, maxID int64) ([]*TopUp, error) {
	orders := []*TopUp{}
	if err := validateWalletRunScope(selection.Scope); err != nil {
		return nil, err
	}
	if cursor < 0 || maxID < cursor || selection.BatchSize < 1 || selection.BatchSize > 100 || selection.QueriesPerSecond < 1 || selection.QueriesPerSecond > 10 {
		return nil, ErrResellerFinanceInvalidInput
	}
	err := walletRunOrders(DB, selection.Scope).Where("id > ? AND id <= ?", cursor, maxID).Order("id ASC").Limit(selection.BatchSize).Find(&orders).Error
	return orders, err
}
func FindActiveWalletRuns() ([]*WalletTopUpRun, error) {
	runs := []*WalletTopUpRun{}
	err := DB.Where("status IN ?", []string{"queued", "running", "pausing"}).Order("updated_at ASC").Order("created_at ASC").Order("id ASC").Limit(10).Find(&runs).Error
	return runs, err
}

func SetWalletRunTask(id, taskID string) error {
	return DB.Model(&WalletTopUpRun{}).Where("id = ?", id).Updates(map[string]any{"last_task_id": taskID, "updated_at": common.GetTimestamp()}).Error
}
func WalletRunTaskLeaseActive(taskID, runner string) (bool, error) {
	var count int64
	err := DB.Model(&SystemTaskLock{}).Where("type = ? AND task_id = ? AND locked_by = ? AND locked_until > ?", "wallet_payment_history", taskID, runner, common.GetTimestamp()).Count(&count).Error
	return count == 1, err
}

// Checkpoints also require the live task lease. Losing the lease or a pause
// invalidates the old worker even if its remote request later succeeds.
func CheckpointWalletRun(run *WalletTopUpRun, taskID, runner string, orderID int64, category string) (bool, error) {
	if run == nil || taskID == "" || runner == "" || orderID <= run.Cursor || orderID > run.MaxOrderID {
		return false, ErrResellerFinanceInvalidInput
	}
	columns := map[string]string{"credited": "credited_count", "closed": "closed_count", "review_required": "review_count", "retry_failed": "error_count"}
	column, ok := columns[category]
	if !ok {
		return false, ErrResellerFinanceInvalidInput
	}
	q := DB.Model(&WalletTopUpRun{}).Where("id = ? AND control_version = ? AND desired_state = ? AND status = ? AND cursor = ?", run.ID, run.ControlVersion, "running", "running", run.Cursor)
	q = q.Where("EXISTS (SELECT 1 FROM system_task_locks WHERE type = ? AND task_id = ? AND locked_by = ? AND locked_until > ?)", "wallet_payment_history", taskID, runner, common.GetTimestamp())
	result := q.Updates(map[string]any{"cursor": orderID, "scanned_count": gorm.Expr("scanned_count + 1"), column: gorm.Expr(column + " + 1"), "updated_at": common.GetTimestamp()})
	if result.Error == nil && result.RowsAffected == 1 {
		run.Cursor = orderID
		run.ScannedCount++
	}
	return result.RowsAffected == 1, result.Error
}

type WalletRunTaskLease struct {
	TaskID string
	Runner string
}

func validateWalletRunScope(scope WalletRunScope) error {
	if scope.TenantID < 0 || scope.CreatedBefore <= 0 || scope.CreatedAfter < 0 || scope.CreatedAfter >= scope.CreatedBefore || strings.TrimSpace(scope.AccountRef) == "" || len(scope.AccountRef) > 128 {
		return ErrResellerFinanceInvalidInput
	}
	switch scope.PaymentProvider {
	case PaymentProviderEpay, PaymentProviderStripe, PaymentProviderCreem, PaymentProviderWaffo, PaymentProviderWaffoPancake, "unknown":
	default:
		return ErrResellerFinanceInvalidInput
	}
	switch scope.Environment {
	case "prod", "test", "unknown":
	default:
		return ErrResellerFinanceInvalidInput
	}
	return nil
}
