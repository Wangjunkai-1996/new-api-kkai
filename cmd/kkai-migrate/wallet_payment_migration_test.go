//go:build !kkai_bridge

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func walletPaymentProofFixture(t *testing.T) (*walletPaymentControllerProof, *walletPaymentMigrationStatus, time.Time) {
	t.Helper()
	contract, err := kkaimigrate.ContractForDialect(kkaimigrate.DialectPostgres)
	require.NoError(t, err)
	contract.RuntimeMaxVersion = 18
	delete(contract.CompatiblePrefixes, "19")
	now := time.Unix(1800000000, 0)
	status := &walletPaymentMigrationStatus{
		Schema: "kkai_wallet_payment_migration_status_v1", CurrentVersion: 17, TargetVersion: 18, Safe: true,
		SourceDigest: contract.CompatiblePrefixes["17"], TargetDigest: contract.CompatiblePrefixes["18"],
		PlanChecksum: kkaimigrate.WalletPaymentPlanChecksum(), DatabaseName: "newapi", DatabaseSchema: "public",
	}
	proof := &walletPaymentControllerProof{tenantControllerProof: tenantControllerProof{
		Schema: "kkai_wallet_payment_migration_proof_v1", Protocol: "wallet-payment-migration-v1", Operation: "apply",
		InstalledInfraSHA: strings.Repeat("a", 40), ImageID: "sha256:" + strings.Repeat("b", 64),
		SourceVersion: 17, TargetVersion: 18, SourceDigest: status.SourceDigest, PlanChecksum: status.PlanChecksum,
		DatabaseName: status.DatabaseName, DatabaseSchema: status.DatabaseSchema, IssuedAt: now.Unix() - 1,
		ExpiresAt: now.Unix() + 119, Nonce: strings.Repeat("c", 32),
	}, TargetDigest: status.TargetDigest}
	for i, role := range []string{"active", "rollback", "writer"} {
		aware, routing, logs := true, true, false
		evidence := walletPaymentRuntimeEvidence{
			tenantRuntimeEvidence: tenantRuntimeEvidence{
				Name: role, Role: role, ContainerID: strings.Repeat(string(rune('a'+i)), 64), ImageID: proof.ImageID,
				Version: "release", TenantRoutingEnabled: &routing, TenantAware: &aware, SeparateLogDatabase: &logs,
			},
			ContainerStartedAt: now.Add(-time.Minute).Format(time.RFC3339Nano), ContainerInitStartTicks: 100,
			ApplicationPID: int64(i + 10), ApplicationStartTicks: 101, ApplicationMountNamespace: "mnt:[123]",
			ApplicationExecutableDevice: 1, ApplicationExecutableInode: 2, ApplicationCgroupSHA256: strings.Repeat("d", 64),
		}
		evidence.SchemaContract.SchemaContract = contract
		evidence.SchemaContract.CompatiblePrefixes = make(map[string]string, len(contract.CompatiblePrefixes))
		for version, digest := range contract.CompatiblePrefixes {
			evidence.SchemaContract.CompatiblePrefixes[version] = digest
		}
		evidence.SchemaContract.SchemaManagement = "external"
		proof.Runtimes = append(proof.Runtimes, evidence)
	}
	return proof, status, now
}

func TestWalletPaymentProofRejectsDriftAndIncompatibleRuntimes(t *testing.T) {
	proof, status, now := walletPaymentProofFixture(t)
	require.NoError(t, validateWalletPaymentControllerProof(proof, status, now))
	encoded, err := common.Marshal(proof)
	require.NoError(t, err)
	var decoded walletPaymentControllerProof
	require.NoError(t, common.Unmarshal(encoded, &decoded))
	require.NoError(t, validateWalletPaymentControllerProof(&decoded, status, now))
	status.CurrentVersion, status.WalletSchemaPresent = 18, true
	require.NoError(t, validateWalletPaymentControllerProof(proof, status, now), "a completed canonical target remains idempotent")
	assert.Error(t, validateWalletPaymentControllerProof(proof, status, now.Add(119*time.Second)))
	for _, test := range []struct {
		name string
		edit func(*walletPaymentControllerProof)
	}{
		{"invoice-protocol", func(p *walletPaymentControllerProof) { p.Protocol = "invoice-migration-v1" }},
		{"invoice-schema", func(p *walletPaymentControllerProof) { p.Schema = "kkai_invoice_migration_proof_v1" }},
		{"wrong-operation", func(p *walletPaymentControllerProof) { p.Operation = "status" }},
		{"wrong-source", func(p *walletPaymentControllerProof) { p.SourceVersion = 16 }},
		{"wrong-target", func(p *walletPaymentControllerProof) { p.TargetVersion = 17 }},
		{"source-digest", func(p *walletPaymentControllerProof) { p.SourceDigest = "other" }},
		{"target-digest", func(p *walletPaymentControllerProof) { p.TargetDigest = "other" }},
		{"compiled-plan", func(p *walletPaymentControllerProof) { p.PlanChecksum = "other" }},
		{"database", func(p *walletPaymentControllerProof) { p.DatabaseName = "other" }},
		{"schema", func(p *walletPaymentControllerProof) { p.DatabaseSchema = "other" }},
		{"infra", func(p *walletPaymentControllerProof) { p.InstalledInfraSHA = "missing" }},
		{"mutable-image", func(p *walletPaymentControllerProof) { p.ImageID = "latest" }},
		{"nonce", func(p *walletPaymentControllerProof) { p.Nonce = "missing" }},
		{"expired", func(p *walletPaymentControllerProof) { p.ExpiresAt = now.Unix() }},
		{"future", func(p *walletPaymentControllerProof) { p.IssuedAt = now.Unix() + 1 }},
		{"overlong", func(p *walletPaymentControllerProof) { p.ExpiresAt++ }},
		{"missing-role", func(p *walletPaymentControllerProof) { p.Runtimes = p.Runtimes[:2] }},
		{"duplicate-role", func(p *walletPaymentControllerProof) { p.Runtimes[1].Role = "active" }},
		{"duplicate-container", func(p *walletPaymentControllerProof) { p.Runtimes[1].ContainerID = p.Runtimes[0].ContainerID }},
		{"process-missing", func(p *walletPaymentControllerProof) { p.Runtimes[1].ApplicationPID = 0 }},
		{"process-repeated", func(p *walletPaymentControllerProof) { p.Runtimes[1].ApplicationPID = p.Runtimes[0].ApplicationPID }},
		{"process-start", func(p *walletPaymentControllerProof) { p.Runtimes[1].ApplicationStartTicks = 99 }},
		{"container-start", func(p *walletPaymentControllerProof) { p.Runtimes[1].ContainerStartedAt = "missing" }},
		{"mount-namespace", func(p *walletPaymentControllerProof) { p.Runtimes[1].ApplicationMountNamespace = "missing" }},
		{"executable", func(p *walletPaymentControllerProof) { p.Runtimes[1].ApplicationExecutableInode = 0 }},
		{"cgroup", func(p *walletPaymentControllerProof) { p.Runtimes[1].ApplicationCgroupSHA256 = "missing" }},
		{"active-image", func(p *walletPaymentControllerProof) { p.Runtimes[0].ImageID = "sha256:" + strings.Repeat("f", 64) }},
		{"rollback-v17", func(p *walletPaymentControllerProof) { p.Runtimes[1].SchemaContract.RuntimeMaxVersion = 17 }},
		{"writer-v17", func(p *walletPaymentControllerProof) { p.Runtimes[2].SchemaContract.RuntimeMaxVersion = 17 }},
		{"unreviewed-max19", func(p *walletPaymentControllerProof) { p.Runtimes[1].SchemaContract.RuntimeMaxVersion = 19 }},
		{"target-prefix", func(p *walletPaymentControllerProof) { p.Runtimes[1].SchemaContract.CompatiblePrefixes["18"] = "other" }},
		{"runtime-management", func(p *walletPaymentControllerProof) { p.Runtimes[1].SchemaContract.SchemaManagement = "runtime" }},
		{"missing-routing-evidence", func(p *walletPaymentControllerProof) { p.Runtimes[1].TenantRoutingEnabled = nil }},
		{"tenant-unaware", func(p *walletPaymentControllerProof) { *p.Runtimes[1].TenantAware = false }},
		{"separate-log-db", func(p *walletPaymentControllerProof) { *p.Runtimes[1].SeparateLogDatabase = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			proof, status, now := walletPaymentProofFixture(t)
			test.edit(proof)
			assert.Error(t, validateWalletPaymentControllerProof(proof, status, now))
		})
	}
	for _, test := range []struct {
		name string
		edit func(*walletPaymentMigrationStatus)
	}{
		{"status-schema", func(s *walletPaymentMigrationStatus) { s.Schema = "other" }},
		{"noncanonical-source", func(s *walletPaymentMigrationStatus) { s.CurrentVersion = 16 }},
		{"unreceipted-objects", func(s *walletPaymentMigrationStatus) { s.WalletSchemaPresent = true }},
		{"incomplete-target", func(s *walletPaymentMigrationStatus) { s.CurrentVersion = 18 }},
		{"unsafe-precheck", func(s *walletPaymentMigrationStatus) { s.Safe = false }},
		{"status-target", func(s *walletPaymentMigrationStatus) { s.TargetVersion = 19 }},
		{"status-source-digest", func(s *walletPaymentMigrationStatus) { s.SourceDigest = "other" }},
		{"status-target-digest", func(s *walletPaymentMigrationStatus) { s.TargetDigest = "other" }},
		{"status-plan", func(s *walletPaymentMigrationStatus) { s.PlanChecksum = "other" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			proof, status, now := walletPaymentProofFixture(t)
			test.edit(status)
			assert.Error(t, validateWalletPaymentControllerProof(proof, status, now))
		})
	}
}

func TestWalletPaymentCommandRequiresItsOwnFixedProof(t *testing.T) {
	for _, operation := range []string{"plan", "status"} {
		require.NoError(t, validateWalletPaymentMigrationCommand(operation, "", true, false))
		assert.Error(t, validateWalletPaymentMigrationCommand(operation, walletPaymentControllerProofPath, true, false))
	}
	require.NoError(t, validateWalletPaymentMigrationCommand("apply", walletPaymentControllerProofPath, true, false))
	for _, path := range []string{"", "/tmp/proof.json", tenantControllerProofPath, invoiceControllerProofPath} {
		assert.Error(t, validateWalletPaymentMigrationCommand("apply", path, true, false))
	}
	assert.Error(t, validateWalletPaymentMigrationCommand("apply", walletPaymentControllerProofPath, false, false))
	assert.Error(t, validateWalletPaymentMigrationCommand("apply", walletPaymentControllerProofPath, true, true))
	assert.Error(t, validateWalletPaymentMigrationCommand("repair", "", true, false))
}

func TestWalletPaymentInspectionRejectsUnreceiptedObjects(t *testing.T) {
	db := newInvoiceCommandFixture(t)
	ctx := context.Background()
	_, err := kkaimigrate.ApplyInvoiceExpand(ctx, db, kkaimigrate.Options{Offline: true})
	require.NoError(t, err)
	for _, ddl := range []string{
		"ALTER TABLE top_ups ADD COLUMN user_id BIGINT NULL",
		"ALTER TABLE top_ups ADD COLUMN create_time BIGINT NULL",
	} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	require.NoError(t, db.Exec("PRAGMA query_only=ON").Error)
	var output bytes.Buffer
	require.NoError(t, runWalletPaymentMigration(ctx, db, "plan", "", &output))
	var status walletPaymentMigrationStatus
	require.NoError(t, common.Unmarshal(output.Bytes(), &status))
	assert.EqualValues(t, 17, status.CurrentVersion)
	assert.False(t, status.WalletSchemaPresent)
	assert.Equal(t, kkaimigrate.WalletPaymentPlanChecksum(), status.PlanChecksum)
	assert.Error(t, runWalletPaymentMigration(ctx, db, "apply", walletPaymentControllerProofPath, &output))
	require.NoError(t, db.Exec("PRAGMA query_only=OFF").Error)
	for _, ddl := range []string{
		"ALTER TABLE top_ups ADD COLUMN currency_exponent INTEGER NULL",
		"CREATE TABLE wallet_topup_runs(id VARCHAR(64) PRIMARY KEY)",
	} {
		tx := db.Begin()
		require.NoError(t, tx.Error)
		require.NoError(t, tx.Exec(ddl).Error)
		_, err = inspectWalletPaymentMigration(ctx, tx)
		assert.ErrorIs(t, err, kkaimigrate.ErrSchemaNotReady)
		require.NoError(t, tx.Rollback().Error)
	}
	_, err = inspectWalletPaymentMigration(ctx, db)
	require.NoError(t, err)
	_, err = kkaimigrate.ApplyWalletPaymentExpand(ctx, db, kkaimigrate.Options{Offline: true})
	require.NoError(t, err)
	var receipt kkaimigrate.AppliedMigration
	require.NoError(t, db.Where("version = ?", 18).Take(&receipt).Error)
	require.NoError(t, db.Where("version = ?", 18).Delete(&receipt).Error)
	_, err = inspectWalletPaymentMigration(ctx, db)
	assert.ErrorIs(t, err, kkaimigrate.ErrSchemaNotReady, "unreceipted expansion cannot be adopted by production apply")
	require.NoError(t, db.Create(&receipt).Error)
	require.NoError(t, db.Exec("PRAGMA query_only=ON").Error)
	output.Reset()
	require.NoError(t, runWalletPaymentMigration(ctx, db, "status", "", &output))
	require.NoError(t, common.Unmarshal(output.Bytes(), &status))
	assert.EqualValues(t, 18, status.CurrentVersion)
	assert.True(t, status.WalletSchemaPresent)
	require.NoError(t, db.Exec("PRAGMA query_only=OFF").Error)
	require.NoError(t, db.Exec("DROP INDEX idx_wallet_topup_request").Error)
	_, err = inspectWalletPaymentMigration(ctx, db)
	assert.ErrorIs(t, err, kkaimigrate.ErrSchemaNotReady, "the canonical receipt cannot conceal lost payment idempotency")
}
