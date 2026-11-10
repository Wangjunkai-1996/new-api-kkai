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

func membershipPaymentProofFixture(t *testing.T) (*membershipPaymentControllerProof, *membershipPaymentMigrationStatus, time.Time) {
	t.Helper()
	contract, err := kkaimigrate.ContractForDialect(kkaimigrate.DialectPostgres)
	require.NoError(t, err)
	contract.RuntimeMaxVersion = 20
	delete(contract.CompatiblePrefixes, "21")
	now := time.Unix(1800000000, 0)
	status := &membershipPaymentMigrationStatus{
		Schema: "kkai_membership_payment_migration_status_v1", CurrentVersion: 19, TargetVersion: 20, Safe: true,
		SourceDigest: contract.CompatiblePrefixes["19"], TargetDigest: contract.CompatiblePrefixes["20"],
		PlanChecksum: kkaimigrate.MembershipPaymentPlanChecksum(), DatabaseName: "newapi", DatabaseSchema: "public",
	}
	proof := &membershipPaymentControllerProof{tenantControllerProof: tenantControllerProof{
		Schema: "kkai_membership_payment_migration_proof_v1", Protocol: "membership-payment-migration-v1", Operation: "apply",
		InstalledInfraSHA: strings.Repeat("a", 40), ImageID: "sha256:" + strings.Repeat("b", 64),
		SourceVersion: 19, TargetVersion: 20, SourceDigest: status.SourceDigest, PlanChecksum: status.PlanChecksum,
		DatabaseName: status.DatabaseName, DatabaseSchema: status.DatabaseSchema, IssuedAt: now.Unix() - 1,
		ExpiresAt: now.Unix() + 119, Nonce: strings.Repeat("c", 32),
	}, TargetDigest: status.TargetDigest}
	for i, role := range []string{"active", "rollback", "writer"} {
		aware, routing, logs := true, true, false
		evidence := membershipPaymentRuntimeEvidence{
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

func TestMembershipPaymentProofRejectsV21AndHistoricalRuntimeSubstitution(t *testing.T) {
	proof, status, now := membershipPaymentProofFixture(t)
	require.Error(t, validateMembershipPaymentControllerProof(proof, status, now), "historical runtime evidence cannot disguise the v21 migration binary")
	current, err := kkaimigrate.ContractForDialect(kkaimigrate.DialectPostgres)
	require.NoError(t, err)
	for i := range proof.Runtimes {
		proof.Runtimes[i].SchemaContract.SchemaContract = current
	}
	require.Error(t, validateMembershipPaymentControllerProof(proof, status, now), "v21 is outside the installed v20 controller contract")
	status.CurrentVersion, status.MembershipPaymentSchemaPresent = 20, true
	require.Error(t, validateMembershipPaymentControllerProof(proof, status, now), "a completed v20 receipt does not broaden controller authority")
}

func TestMembershipPaymentProofRejectsDriftAndIncompatibleRuntimes(t *testing.T) {
	requireHistoricalMigrationRuntime(t)
	proof, status, now := membershipPaymentProofFixture(t)
	expected := proof.Runtimes[0].SchemaContract.SchemaContract
	require.NoError(t, validateMembershipPaymentProofAgainstContract(proof, status, now, expected))
	encoded, err := common.Marshal(proof)
	require.NoError(t, err)
	var decoded membershipPaymentControllerProof
	require.NoError(t, common.Unmarshal(encoded, &decoded))
	require.NoError(t, validateMembershipPaymentProofAgainstContract(&decoded, status, now, expected))
	status.CurrentVersion, status.MembershipPaymentSchemaPresent = 20, true
	require.NoError(t, validateMembershipPaymentProofAgainstContract(proof, status, now, expected), "a completed canonical target remains idempotent")
	assert.Error(t, validateMembershipPaymentProofAgainstContract(proof, status, now.Add(119*time.Second), expected))
	for _, test := range []struct {
		name string
		edit func(*membershipPaymentControllerProof)
	}{
		{"invoice-protocol", func(p *membershipPaymentControllerProof) { p.Protocol = "invoice-migration-v1" }},
		{"invoice-schema", func(p *membershipPaymentControllerProof) { p.Schema = "kkai_invoice_migration_proof_v1" }},
		{"wrong-operation", func(p *membershipPaymentControllerProof) { p.Operation = "status" }},
		{"wrong-source", func(p *membershipPaymentControllerProof) { p.SourceVersion = 17 }},
		{"wrong-target", func(p *membershipPaymentControllerProof) { p.TargetVersion = 19 }},
		{"source-digest", func(p *membershipPaymentControllerProof) { p.SourceDigest = "other" }},
		{"target-digest", func(p *membershipPaymentControllerProof) { p.TargetDigest = "other" }},
		{"compiled-plan", func(p *membershipPaymentControllerProof) { p.PlanChecksum = "other" }},
		{"database", func(p *membershipPaymentControllerProof) { p.DatabaseName = "other" }},
		{"schema", func(p *membershipPaymentControllerProof) { p.DatabaseSchema = "other" }},
		{"infra", func(p *membershipPaymentControllerProof) { p.InstalledInfraSHA = "missing" }},
		{"mutable-image", func(p *membershipPaymentControllerProof) { p.ImageID = "latest" }},
		{"nonce", func(p *membershipPaymentControllerProof) { p.Nonce = "missing" }},
		{"expired", func(p *membershipPaymentControllerProof) { p.ExpiresAt = now.Unix() }},
		{"future", func(p *membershipPaymentControllerProof) { p.IssuedAt = now.Unix() + 1 }},
		{"overlong", func(p *membershipPaymentControllerProof) { p.ExpiresAt++ }},
		{"missing-role", func(p *membershipPaymentControllerProof) { p.Runtimes = p.Runtimes[:2] }},
		{"duplicate-role", func(p *membershipPaymentControllerProof) { p.Runtimes[1].Role = "active" }},
		{"duplicate-container", func(p *membershipPaymentControllerProof) { p.Runtimes[1].ContainerID = p.Runtimes[0].ContainerID }},
		{"process-missing", func(p *membershipPaymentControllerProof) { p.Runtimes[1].ApplicationPID = 0 }},
		{"process-repeated", func(p *membershipPaymentControllerProof) { p.Runtimes[1].ApplicationPID = p.Runtimes[0].ApplicationPID }},
		{"process-start", func(p *membershipPaymentControllerProof) { p.Runtimes[1].ApplicationStartTicks = 99 }},
		{"container-start", func(p *membershipPaymentControllerProof) { p.Runtimes[1].ContainerStartedAt = "missing" }},
		{"mount-namespace", func(p *membershipPaymentControllerProof) { p.Runtimes[1].ApplicationMountNamespace = "missing" }},
		{"executable", func(p *membershipPaymentControllerProof) { p.Runtimes[1].ApplicationExecutableInode = 0 }},
		{"cgroup", func(p *membershipPaymentControllerProof) { p.Runtimes[1].ApplicationCgroupSHA256 = "missing" }},
		{"active-image", func(p *membershipPaymentControllerProof) { p.Runtimes[0].ImageID = "sha256:" + strings.Repeat("f", 64) }},
		{"rollback-v19", func(p *membershipPaymentControllerProof) { p.Runtimes[1].SchemaContract.RuntimeMaxVersion = 19 }},
		{"writer-v19", func(p *membershipPaymentControllerProof) { p.Runtimes[2].SchemaContract.RuntimeMaxVersion = 19 }},
		{"unreviewed-max21", func(p *membershipPaymentControllerProof) { p.Runtimes[1].SchemaContract.RuntimeMaxVersion = 21 }},
		{"target-prefix", func(p *membershipPaymentControllerProof) {
			p.Runtimes[1].SchemaContract.CompatiblePrefixes["20"] = "other"
		}},
		{"runtime-management", func(p *membershipPaymentControllerProof) { p.Runtimes[1].SchemaContract.SchemaManagement = "runtime" }},
		{"missing-routing-evidence", func(p *membershipPaymentControllerProof) { p.Runtimes[1].TenantRoutingEnabled = nil }},
		{"tenant-unaware", func(p *membershipPaymentControllerProof) { *p.Runtimes[1].TenantAware = false }},
		{"separate-log-db", func(p *membershipPaymentControllerProof) { *p.Runtimes[1].SeparateLogDatabase = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			proof, status, now := membershipPaymentProofFixture(t)
			test.edit(proof)
			assert.Error(t, validateMembershipPaymentProofAgainstContract(proof, status, now, expected))
		})
	}
	for _, test := range []struct {
		name string
		edit func(*membershipPaymentMigrationStatus)
	}{
		{"status-schema", func(s *membershipPaymentMigrationStatus) { s.Schema = "other" }},
		{"noncanonical-source", func(s *membershipPaymentMigrationStatus) { s.CurrentVersion = 17 }},
		{"unreceipted-objects", func(s *membershipPaymentMigrationStatus) { s.MembershipPaymentSchemaPresent = true }},
		{"incomplete-target", func(s *membershipPaymentMigrationStatus) { s.CurrentVersion = 20 }},
		{"unsafe-precheck", func(s *membershipPaymentMigrationStatus) { s.Safe = false }},
		{"status-target", func(s *membershipPaymentMigrationStatus) { s.TargetVersion = 21 }},
		{"status-source-digest", func(s *membershipPaymentMigrationStatus) { s.SourceDigest = "other" }},
		{"status-target-digest", func(s *membershipPaymentMigrationStatus) { s.TargetDigest = "other" }},
		{"status-plan", func(s *membershipPaymentMigrationStatus) { s.PlanChecksum = "other" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			proof, status, now := membershipPaymentProofFixture(t)
			test.edit(status)
			assert.Error(t, validateMembershipPaymentProofAgainstContract(proof, status, now, expected))
		})
	}
}

func TestMembershipPaymentCommandRequiresItsOwnFixedProof(t *testing.T) {
	for _, operation := range []string{"plan", "status"} {
		require.NoError(t, validateMembershipPaymentMigrationCommand(operation, "", true, false))
		assert.Error(t, validateMembershipPaymentMigrationCommand(operation, membershipPaymentControllerProofPath, true, false))
	}
	require.NoError(t, validateMembershipPaymentMigrationCommand("apply", membershipPaymentControllerProofPath, true, false))
	for _, path := range []string{"", "/tmp/proof.json", tenantControllerProofPath, invoiceControllerProofPath, walletPaymentControllerProofPath, shopControllerProofPath} {
		assert.Error(t, validateMembershipPaymentMigrationCommand("apply", path, true, false))
	}
	assert.Error(t, validateMembershipPaymentMigrationCommand("apply", membershipPaymentControllerProofPath, false, false))
	assert.Error(t, validateMembershipPaymentMigrationCommand("apply", membershipPaymentControllerProofPath, true, true))
	assert.Error(t, validateMembershipPaymentMigrationCommand("repair", "", true, false))
}

func TestMembershipPaymentInspectionIsReadonlyAndRejectsUnreceiptedObjects(t *testing.T) {
	requireHistoricalMigrationRuntime(t)
	db := newInvoiceCommandFixture(t)
	ctx := context.Background()
	_, err := kkaimigrate.ApplyInvoiceExpand(ctx, db, kkaimigrate.Options{Offline: true})
	require.NoError(t, err)
	for _, ddl := range []string{"ALTER TABLE top_ups ADD COLUMN user_id BIGINT NULL", "ALTER TABLE top_ups ADD COLUMN create_time BIGINT NULL"} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	_, err = kkaimigrate.ApplyWalletPaymentExpand(ctx, db, kkaimigrate.Options{Offline: true})
	require.NoError(t, err)
	_, err = kkaimigrate.ApplyShopExpand(ctx, db, kkaimigrate.Options{Offline: true})
	require.NoError(t, err)
	require.NoError(t, db.Exec("PRAGMA query_only=ON").Error)
	var output bytes.Buffer
	require.NoError(t, runMigrationPrecheck(ctx, db, kkaimigrate.MembershipPaymentSchemaVersion, &output))
	var precheck kkaimigrate.MembershipPaymentExpandPrecheck
	require.NoError(t, common.Unmarshal(output.Bytes(), &precheck))
	assert.EqualValues(t, 19, precheck.CurrentVersion)
	assert.EqualValues(t, 20, precheck.TargetVersion)
	planned, err := applyMigrationTarget(ctx, db, kkaimigrate.MembershipPaymentSchemaVersion, kkaimigrate.Options{DryRun: true})
	require.NoError(t, err)
	require.Len(t, planned.Pending, 1)
	assert.EqualValues(t, 20, planned.Pending[0].Version)
	output.Reset()
	assert.ErrorIs(t, runMembershipPaymentMigration(ctx, db, "plan", "", &output), kkaimigrate.ErrUnsafeMigration, "v21 is outside the installed v20 controller contract")
	assert.Error(t, runMembershipPaymentMigration(ctx, db, "apply", membershipPaymentControllerProofPath, &output), "production mutation has no SQLite fallback")
	assert.False(t, db.Migrator().HasTable("kkai_membership_payment_checks"))
	require.NoError(t, db.Exec("PRAGMA query_only=OFF").Error)
	tx := db.Begin()
	require.NoError(t, tx.Error)
	require.NoError(t, tx.Exec("CREATE TABLE kkai_membership_payment_checks(order_id BIGINT PRIMARY KEY)").Error)
	_, err = inspectMembershipPaymentMigration(ctx, tx)
	assert.ErrorIs(t, err, kkaimigrate.ErrSchemaNotReady)
	require.NoError(t, tx.Rollback().Error)
	_, err = kkaimigrate.ApplyMembershipPaymentExpand(ctx, db, kkaimigrate.Options{Offline: true})
	require.NoError(t, err)
	_, err = inspectMembershipPaymentMigration(ctx, db)
	require.ErrorIs(t, err, kkaimigrate.ErrUnsafeMigration)
	statusAfter, err := kkaimigrate.PrecheckMembershipPaymentExpand(ctx, db)
	require.NoError(t, err)
	assert.EqualValues(t, 20, statusAfter.CurrentVersion)
	require.NoError(t, kkaimigrate.CheckMembershipPaymentSchema(ctx, db))
	var receipt kkaimigrate.AppliedMigration
	require.NoError(t, db.Where("version = ?", 20).Take(&receipt).Error)
	require.NoError(t, db.Where("version = ?", 20).Delete(&receipt).Error)
	_, err = inspectMembershipPaymentMigration(ctx, db)
	assert.ErrorIs(t, err, kkaimigrate.ErrSchemaNotReady, "DDL without its canonical receipt never authorizes payment recovery")
}
