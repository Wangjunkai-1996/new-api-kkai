package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
)

func RegisterResellerFinanceBackgroundJobs(registry *BackgroundJobRegistry) error {
	if registry == nil {
		return ErrInvalidBackgroundJob
	}
	// Freeze readiness at startup, matching the reseller HTTP gate. A bridge
	// writer must be replaced after contract acceptance before it can settle
	// tenant requests or release profit, even if the database advances later.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	ready := kkaimigrate.CheckTenantOnlineSchema(ctx, model.DB) == nil
	cancel()
	if !ready {
		return nil
	}
	if err := RegisterTenantConsumptionBackgroundJob(registry); err != nil {
		return err
	}
	return registry.Register(BackgroundJob{
		Name: "reseller-profit-release", Interval: time.Minute, RunOnStart: true,
		WritesData: true, RequiresLeaderLease: true,
		Run: RunResellerPendingRelease,
	})
}

// RunResellerPendingRelease bounds each pass and reuses the writer/leader gates
// of the application job registry. Processing one tenant is one transaction.
func RunResellerPendingRelease(ctx context.Context) error {
	if model.DB == nil {
		return model.ErrTenantStoreUnavailable
	}
	db := model.DB.WithContext(ctx)
	processed := db.Model(&model.ResellerLedgerEntry{}).Select("reversal_of").Where("kind IN ?", []string{model.ResellerLedgerKindPendingRelease, model.ResellerLedgerKindChargeback})
	var tenants []int64
	now := time.Now().Unix()
	if err := db.Model(&model.ResellerLedgerEntry{}).Distinct("tenant_id").Where("kind = ? AND available_at <= ? AND operation_id NOT IN (?)", model.ResellerLedgerKindSettlement, now, processed).Order("tenant_id").Limit(100).Pluck("tenant_id", &tenants).Error; err != nil {
		return err
	}
	finance := NewResellerFinanceService(model.DB)
	var failures []error
	for _, tenantID := range tenants {
		if _, err := finance.ReleasePending(ctx, tenantID, now, 100); err != nil {
			failures = append(failures, fmt.Errorf("tenant %d pending release: %w", tenantID, err))
		}
	}
	return errors.Join(failures...)
}
