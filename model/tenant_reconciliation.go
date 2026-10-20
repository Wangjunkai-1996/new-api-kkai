package model

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const ResellerLedgerKindUsageReconciliation = "usage_reconciliation"

type TenantConsumptionReconciliationInput struct {
	TenantID                  int64  `json:"tenant_id"`
	RequestID                 string `json:"request_id"`
	IdempotencyKey            string `json:"idempotency_key"`
	ActualQuota               int64  `json:"actual_quota"`
	ConfirmedZero             bool   `json:"confirmed_zero"`
	ConfirmedExecutionStopped bool   `json:"confirmed_execution_stopped"`
	ActorID                   int    `json:"actor_id"`
	Reason                    string `json:"reason"`
	Evidence                  string `json:"evidence"`
	Now                       int64  `json:"-"`
}

// ReconcileTenantConsumptionTx records an operator-confirmed usage result.
// It never moves money or quota: the existing settlement worker completes the
// durable intent. Lock order matches settlement: consumption -> user -> wallet.
// An unmarked reservation needs an explicit operator confirmation that its
// original execution stopped; request age alone never permits reconciliation.
func ReconcileTenantConsumptionTx(tx *gorm.DB, input TenantConsumptionReconciliationInput) (*ResellerLedgerEntry, error) {
	if tx == nil || input.TenantID <= 0 || input.ActorID <= 0 || strings.TrimSpace(input.RequestID) == "" || len(input.RequestID) > 128 || strings.TrimSpace(input.IdempotencyKey) == "" || len(input.IdempotencyKey) > 128 || strings.TrimSpace(input.Reason) == "" || len(input.Reason) > 4096 || strings.TrimSpace(input.Evidence) == "" || len(input.Evidence) > 4096 || input.ActualQuota < 0 || input.ActualQuota > math.MaxInt32 || input.ConfirmedZero != (input.ActualQuota == 0) {
		return nil, ErrResellerFinanceInvalidInput
	}
	var consumption TenantConsumption
	if err := lockForUpdate(tx).Where("tenant_id = ? AND request_id = ?", input.TenantID, input.RequestID).First(&consumption).Error; err != nil {
		return nil, err
	}
	if consumption.QuoteID == 0 && input.ActualQuota > 0 {
		return nil, ErrResellerFinanceInvalidInput
	}
	var user User
	if err := lockForUpdate(tx).Where("tenant_id = ? AND id = ?", input.TenantID, consumption.CustomerID).First(&user).Error; err != nil {
		return nil, err
	}
	var wallet ResellerWallet
	if err := lockForUpdate(tx).Where("tenant_id = ?", input.TenantID).First(&wallet).Error; err != nil {
		return nil, err
	}
	hash, err := financePayloadHash(input)
	if err != nil {
		return nil, err
	}
	operationID := fmt.Sprintf("usage_reconciliation:%d:%s", input.TenantID, input.IdempotencyKey)
	var existing ResellerLedgerEntry
	if err := tx.Where("operation_id = ?", operationID).First(&existing).Error; err == nil {
		if existing.TenantID != input.TenantID || existing.SourceReference != input.RequestID || existing.QuoteID != consumption.QuoteID || existing.PayloadHash != hash {
			return nil, ErrResellerLedgerConflict
		}
		return &existing, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	uncertain := consumption.LastError == "upstream_final_usage_unknown"
	confirmedStopped := consumption.LastError == "" && input.ConfirmedExecutionStopped
	if consumption.Status != "reserved" || (!uncertain && !confirmedStopped) || consumption.QuoteID < 0 {
		return nil, ErrTenantConsumptionConflict
	}
	evidence, err := common.Marshal(input)
	if err != nil {
		return nil, err
	}
	now := normalizeFinanceNow(input.Now)
	entry := &ResellerLedgerEntry{
		TenantID: input.TenantID, WalletID: wallet.ID, OperationID: operationID,
		Kind: ResellerLedgerKindUsageReconciliation, QuoteID: consumption.QuoteID,
		SourceReference: input.RequestID, PayloadHash: hash, ActorID: input.ActorID,
		Reason: string(evidence), CreatedAt: now, SupplierMinorExact: "0",
		ExactProfitMinor: "0", PaymentAllocations: "[]",
	}
	if err := tx.Create(entry).Error; err != nil {
		return nil, err
	}
	status := "settlement_pending"
	if input.ConfirmedZero {
		status = "refund_pending"
	}
	if err := tx.Model(&consumption).Updates(map[string]any{"status": status, "actual_quota": input.ActualQuota, "last_error": "", "updated_at": now}).Error; err != nil {
		return nil, err
	}
	return entry, nil
}
