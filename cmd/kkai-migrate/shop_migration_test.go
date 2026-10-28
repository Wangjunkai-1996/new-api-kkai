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

func shopProofFixture(t *testing.T) (*shopControllerProof, *shopMigrationStatus, time.Time) {
	t.Helper()
	contract, err := kkaimigrate.ContractForDialect(kkaimigrate.DialectPostgres)
	require.NoError(t, err)
	now := time.Unix(1800000000, 0)
	status := &shopMigrationStatus{
		Schema: "kkai_shop_migration_status_v1", CurrentVersion: 18, TargetVersion: 19, Safe: true,
		SourceDigest: contract.CompatiblePrefixes["18"], TargetDigest: contract.CompatiblePrefixes["19"],
		PlanChecksum: kkaimigrate.ShopPlanChecksum(), DatabaseName: "newapi", DatabaseSchema: "public",
	}
	proof := &shopControllerProof{tenantControllerProof: tenantControllerProof{
		Schema: "kkai_shop_migration_proof_v1", Protocol: "shop-migration-v1", Operation: "apply",
		InstalledInfraSHA: strings.Repeat("a", 40), ImageID: "sha256:" + strings.Repeat("b", 64),
		SourceVersion: 18, TargetVersion: 19, SourceDigest: status.SourceDigest, PlanChecksum: status.PlanChecksum,
		DatabaseName: status.DatabaseName, DatabaseSchema: status.DatabaseSchema, IssuedAt: now.Unix() - 1,
		ExpiresAt: now.Unix() + 119, Nonce: strings.Repeat("c", 32),
	}, TargetDigest: status.TargetDigest}
	for i, role := range []string{"active", "rollback", "writer"} {
		aware, routing, logs := true, true, false
		evidence := shopRuntimeEvidence{
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

func TestShopProofRejectsDriftAndIncompatibleRuntimes(t *testing.T) {
	proof, status, now := shopProofFixture(t)
	require.NoError(t, validateShopControllerProof(proof, status, now))
	encoded, err := common.Marshal(proof)
	require.NoError(t, err)
	var decoded shopControllerProof
	require.NoError(t, common.Unmarshal(encoded, &decoded))
	require.NoError(t, validateShopControllerProof(&decoded, status, now))
	status.CurrentVersion, status.ShopSchemaPresent = 19, true
	require.NoError(t, validateShopControllerProof(proof, status, now), "a completed canonical target remains idempotent")
	assert.Error(t, validateShopControllerProof(proof, status, now.Add(119*time.Second)))
	for _, test := range []struct {
		name string
		edit func(*shopControllerProof)
	}{
		{"invoice-protocol", func(p *shopControllerProof) { p.Protocol = "invoice-migration-v1" }},
		{"invoice-schema", func(p *shopControllerProof) { p.Schema = "kkai_invoice_migration_proof_v1" }},
		{"wrong-operation", func(p *shopControllerProof) { p.Operation = "status" }},
		{"wrong-source", func(p *shopControllerProof) { p.SourceVersion = 17 }},
		{"wrong-target", func(p *shopControllerProof) { p.TargetVersion = 18 }},
		{"source-digest", func(p *shopControllerProof) { p.SourceDigest = "other" }},
		{"target-digest", func(p *shopControllerProof) { p.TargetDigest = "other" }},
		{"compiled-plan", func(p *shopControllerProof) { p.PlanChecksum = "other" }},
		{"database", func(p *shopControllerProof) { p.DatabaseName = "other" }},
		{"schema", func(p *shopControllerProof) { p.DatabaseSchema = "other" }},
		{"infra", func(p *shopControllerProof) { p.InstalledInfraSHA = "missing" }},
		{"mutable-image", func(p *shopControllerProof) { p.ImageID = "latest" }},
		{"nonce", func(p *shopControllerProof) { p.Nonce = "missing" }},
		{"expired", func(p *shopControllerProof) { p.ExpiresAt = now.Unix() }},
		{"future", func(p *shopControllerProof) { p.IssuedAt = now.Unix() + 1 }},
		{"overlong", func(p *shopControllerProof) { p.ExpiresAt++ }},
		{"missing-role", func(p *shopControllerProof) { p.Runtimes = p.Runtimes[:2] }},
		{"duplicate-role", func(p *shopControllerProof) { p.Runtimes[1].Role = "active" }},
		{"duplicate-container", func(p *shopControllerProof) { p.Runtimes[1].ContainerID = p.Runtimes[0].ContainerID }},
		{"process-missing", func(p *shopControllerProof) { p.Runtimes[1].ApplicationPID = 0 }},
		{"process-repeated", func(p *shopControllerProof) { p.Runtimes[1].ApplicationPID = p.Runtimes[0].ApplicationPID }},
		{"process-start", func(p *shopControllerProof) { p.Runtimes[1].ApplicationStartTicks = 99 }},
		{"container-start", func(p *shopControllerProof) { p.Runtimes[1].ContainerStartedAt = "missing" }},
		{"mount-namespace", func(p *shopControllerProof) { p.Runtimes[1].ApplicationMountNamespace = "missing" }},
		{"executable", func(p *shopControllerProof) { p.Runtimes[1].ApplicationExecutableInode = 0 }},
		{"cgroup", func(p *shopControllerProof) { p.Runtimes[1].ApplicationCgroupSHA256 = "missing" }},
		{"active-image", func(p *shopControllerProof) { p.Runtimes[0].ImageID = "sha256:" + strings.Repeat("f", 64) }},
		{"rollback-v18", func(p *shopControllerProof) { p.Runtimes[1].SchemaContract.RuntimeMaxVersion = 18 }},
		{"writer-v18", func(p *shopControllerProof) { p.Runtimes[2].SchemaContract.RuntimeMaxVersion = 18 }},
		{"unreviewed-max20", func(p *shopControllerProof) { p.Runtimes[1].SchemaContract.RuntimeMaxVersion = 20 }},
		{"target-prefix", func(p *shopControllerProof) { p.Runtimes[1].SchemaContract.CompatiblePrefixes["19"] = "other" }},
		{"runtime-management", func(p *shopControllerProof) { p.Runtimes[1].SchemaContract.SchemaManagement = "runtime" }},
		{"missing-routing-evidence", func(p *shopControllerProof) { p.Runtimes[1].TenantRoutingEnabled = nil }},
		{"tenant-unaware", func(p *shopControllerProof) { *p.Runtimes[1].TenantAware = false }},
		{"separate-log-db", func(p *shopControllerProof) { *p.Runtimes[1].SeparateLogDatabase = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			proof, status, now := shopProofFixture(t)
			test.edit(proof)
			assert.Error(t, validateShopControllerProof(proof, status, now))
		})
	}
	for _, test := range []struct {
		name string
		edit func(*shopMigrationStatus)
	}{
		{"status-schema", func(s *shopMigrationStatus) { s.Schema = "other" }},
		{"noncanonical-source", func(s *shopMigrationStatus) { s.CurrentVersion = 17 }},
		{"unreceipted-objects", func(s *shopMigrationStatus) { s.ShopSchemaPresent = true }},
		{"incomplete-target", func(s *shopMigrationStatus) { s.CurrentVersion = 19 }},
		{"unsafe-precheck", func(s *shopMigrationStatus) { s.Safe = false }},
		{"status-target", func(s *shopMigrationStatus) { s.TargetVersion = 20 }},
		{"status-source-digest", func(s *shopMigrationStatus) { s.SourceDigest = "other" }},
		{"status-target-digest", func(s *shopMigrationStatus) { s.TargetDigest = "other" }},
		{"status-plan", func(s *shopMigrationStatus) { s.PlanChecksum = "other" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			proof, status, now := shopProofFixture(t)
			test.edit(status)
			assert.Error(t, validateShopControllerProof(proof, status, now))
		})
	}
}

func TestShopCommandRequiresItsOwnFixedProof(t *testing.T) {
	for _, operation := range []string{"plan", "status"} {
		require.NoError(t, validateShopMigrationCommand(operation, "", true, false))
		assert.Error(t, validateShopMigrationCommand(operation, shopControllerProofPath, true, false))
	}
	require.NoError(t, validateShopMigrationCommand("apply", shopControllerProofPath, true, false))
	for _, path := range []string{"", "/tmp/proof.json", tenantControllerProofPath, invoiceControllerProofPath, walletPaymentControllerProofPath} {
		assert.Error(t, validateShopMigrationCommand("apply", path, true, false))
	}
	assert.Error(t, validateShopMigrationCommand("apply", shopControllerProofPath, false, false))
	assert.Error(t, validateShopMigrationCommand("apply", shopControllerProofPath, true, true))
	assert.Error(t, validateShopMigrationCommand("repair", "", true, false))
}

func TestShopInspectionIsReadonlyAndRejectsUnreceiptedObjects(t *testing.T) {
	db := newInvoiceCommandFixture(t)
	ctx := context.Background()
	_, err := kkaimigrate.ApplyInvoiceExpand(ctx, db, kkaimigrate.Options{Offline: true})
	require.NoError(t, err)
	for _, ddl := range []string{"ALTER TABLE top_ups ADD COLUMN user_id BIGINT NULL", "ALTER TABLE top_ups ADD COLUMN create_time BIGINT NULL"} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	_, err = kkaimigrate.ApplyWalletPaymentExpand(ctx, db, kkaimigrate.Options{Offline: true})
	require.NoError(t, err)
	require.NoError(t, db.Exec("PRAGMA query_only=ON").Error)
	var output bytes.Buffer
	require.NoError(t, runShopMigration(ctx, db, "plan", "", &output))
	var status shopMigrationStatus
	require.NoError(t, common.Unmarshal(output.Bytes(), &status))
	assert.EqualValues(t, 18, status.CurrentVersion)
	assert.False(t, status.ShopSchemaPresent)
	assert.Equal(t, kkaimigrate.ShopPlanChecksum(), status.PlanChecksum)
	assert.Error(t, runShopMigration(ctx, db, "apply", shopControllerProofPath, &output), "production mutation has no SQLite fallback")
	assert.False(t, db.Migrator().HasTable("shop_orders"))
	require.NoError(t, db.Exec("PRAGMA query_only=OFF").Error)
	tx := db.Begin()
	require.NoError(t, tx.Error)
	require.NoError(t, tx.Exec("CREATE TABLE shop_orders(id BIGINT PRIMARY KEY)").Error)
	_, err = inspectShopMigration(ctx, tx)
	assert.ErrorIs(t, err, kkaimigrate.ErrSchemaNotReady)
	require.NoError(t, tx.Rollback().Error)
	_, err = kkaimigrate.ApplyShopExpand(ctx, db, kkaimigrate.Options{Offline: true})
	require.NoError(t, err)
	statusAfter, err := inspectShopMigration(ctx, db)
	require.NoError(t, err)
	assert.EqualValues(t, 19, statusAfter.CurrentVersion)
	assert.True(t, statusAfter.ShopSchemaPresent)
	var receipt kkaimigrate.AppliedMigration
	require.NoError(t, db.Where("version = ?", 19).Take(&receipt).Error)
	require.NoError(t, db.Where("version = ?", 19).Delete(&receipt).Error)
	_, err = inspectShopMigration(ctx, db)
	assert.ErrorIs(t, err, kkaimigrate.ErrSchemaNotReady, "DDL without its canonical receipt never authorizes commerce")
}
