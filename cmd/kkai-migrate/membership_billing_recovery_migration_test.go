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

func membershipBillingRecoveryProofFixture(t *testing.T) (*membershipBillingRecoveryControllerProof, *membershipBillingRecoveryMigrationStatus, time.Time) {
	t.Helper()
	contract, err := kkaimigrate.ContractForDialect(kkaimigrate.DialectPostgres)
	require.NoError(t, err)
	now := time.Unix(1800000000, 0)
	status := &membershipBillingRecoveryMigrationStatus{
		Schema: "kkai_membership_billing_recovery_migration_status_v1", CurrentVersion: 20, TargetVersion: 21, Safe: true,
		SourceDigest: contract.CompatiblePrefixes["20"], TargetDigest: contract.CompatiblePrefixes["21"],
		PlanChecksum: kkaimigrate.MembershipBillingRecoveryPlanChecksum(), DatabaseName: "newapi", DatabaseSchema: "public",
	}
	proof := &membershipBillingRecoveryControllerProof{tenantControllerProof: tenantControllerProof{
		Schema: "kkai_membership_billing_recovery_migration_proof_v1", Protocol: "membership-billing-recovery-migration-v1", Operation: "apply",
		InstalledInfraSHA: strings.Repeat("a", 40), ImageID: "sha256:" + strings.Repeat("b", 64),
		SourceVersion: 20, TargetVersion: 21, SourceDigest: status.SourceDigest, PlanChecksum: status.PlanChecksum,
		DatabaseName: status.DatabaseName, DatabaseSchema: status.DatabaseSchema, IssuedAt: now.Unix() - 1,
		ExpiresAt: now.Unix() + 119, Nonce: strings.Repeat("c", 32),
	}, TargetDigest: status.TargetDigest}
	for i, role := range []string{"active", "rollback", "writer"} {
		aware, routing, logs := true, true, false
		evidence := membershipBillingRecoveryRuntimeEvidence{
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

func TestMembershipBillingRecoveryProofRejectsDriftAndIncompatibleRuntimes(t *testing.T) {
	proof, status, now := membershipBillingRecoveryProofFixture(t)
	// These values are frozen into the independently installed root controller.
	assert.Equal(t, "sha256:aec42c828819055a9f678820802cc16e3ef47eee28ffb9308322a02063911a45", status.SourceDigest)
	assert.Equal(t, "sha256:84687631f06a71630ad6035a28ce29c91bd24fb5cf824e72e394b7f8fc0680f0", status.TargetDigest)
	assert.Equal(t, "b9197ded1e980101a66510760b9172e4c924adff54ec8a47eabf972faec4a92a", status.PlanChecksum)
	require.NoError(t, validateMembershipBillingRecoveryControllerProof(proof, status, now))
	encoded, err := common.Marshal(proof)
	require.NoError(t, err)
	var decoded membershipBillingRecoveryControllerProof
	require.NoError(t, common.Unmarshal(encoded, &decoded))
	require.NoError(t, validateMembershipBillingRecoveryControllerProof(&decoded, status, now))
	status.CurrentVersion, status.MembershipBillingRecoverySchemaPresent = 21, true
	require.NoError(t, validateMembershipBillingRecoveryControllerProof(proof, status, now), "a completed canonical target remains idempotent")
	assert.Error(t, validateMembershipBillingRecoveryControllerProof(proof, status, now.Add(119*time.Second)))
	for _, test := range []struct {
		name string
		edit func(*membershipBillingRecoveryControllerProof)
	}{
		{"payment-protocol", func(p *membershipBillingRecoveryControllerProof) { p.Protocol = "membership-payment-migration-v1" }},
		{"payment-schema", func(p *membershipBillingRecoveryControllerProof) {
			p.Schema = "kkai_membership_payment_migration_proof_v1"
		}},
		{"invoice-protocol", func(p *membershipBillingRecoveryControllerProof) { p.Protocol = "invoice-migration-v1" }},
		{"invoice-schema", func(p *membershipBillingRecoveryControllerProof) { p.Schema = "kkai_invoice_migration_proof_v1" }},
		{"wrong-operation", func(p *membershipBillingRecoveryControllerProof) { p.Operation = "status" }},
		{"wrong-source", func(p *membershipBillingRecoveryControllerProof) { p.SourceVersion = 17 }},
		{"wrong-target", func(p *membershipBillingRecoveryControllerProof) { p.TargetVersion = 20 }},
		{"source-digest", func(p *membershipBillingRecoveryControllerProof) { p.SourceDigest = "other" }},
		{"target-digest", func(p *membershipBillingRecoveryControllerProof) { p.TargetDigest = "other" }},
		{"compiled-plan", func(p *membershipBillingRecoveryControllerProof) { p.PlanChecksum = "other" }},
		{"database", func(p *membershipBillingRecoveryControllerProof) { p.DatabaseName = "other" }},
		{"schema", func(p *membershipBillingRecoveryControllerProof) { p.DatabaseSchema = "other" }},
		{"infra", func(p *membershipBillingRecoveryControllerProof) { p.InstalledInfraSHA = "missing" }},
		{"mutable-image", func(p *membershipBillingRecoveryControllerProof) { p.ImageID = "latest" }},
		{"nonce", func(p *membershipBillingRecoveryControllerProof) { p.Nonce = "missing" }},
		{"expired", func(p *membershipBillingRecoveryControllerProof) { p.ExpiresAt = now.Unix() }},
		{"future", func(p *membershipBillingRecoveryControllerProof) { p.IssuedAt = now.Unix() + 1 }},
		{"overlong", func(p *membershipBillingRecoveryControllerProof) { p.ExpiresAt++ }},
		{"missing-role", func(p *membershipBillingRecoveryControllerProof) { p.Runtimes = p.Runtimes[:2] }},
		{"duplicate-role", func(p *membershipBillingRecoveryControllerProof) { p.Runtimes[1].Role = "active" }},
		{"duplicate-container", func(p *membershipBillingRecoveryControllerProof) {
			p.Runtimes[1].ContainerID = p.Runtimes[0].ContainerID
		}},
		{"process-missing", func(p *membershipBillingRecoveryControllerProof) { p.Runtimes[1].ApplicationPID = 0 }},
		{"process-repeated", func(p *membershipBillingRecoveryControllerProof) {
			p.Runtimes[1].ApplicationPID = p.Runtimes[0].ApplicationPID
		}},
		{"process-start", func(p *membershipBillingRecoveryControllerProof) { p.Runtimes[1].ApplicationStartTicks = 99 }},
		{"container-start", func(p *membershipBillingRecoveryControllerProof) { p.Runtimes[1].ContainerStartedAt = "missing" }},
		{"mount-namespace", func(p *membershipBillingRecoveryControllerProof) { p.Runtimes[1].ApplicationMountNamespace = "missing" }},
		{"executable", func(p *membershipBillingRecoveryControllerProof) { p.Runtimes[1].ApplicationExecutableInode = 0 }},
		{"cgroup", func(p *membershipBillingRecoveryControllerProof) { p.Runtimes[1].ApplicationCgroupSHA256 = "missing" }},
		{"active-image", func(p *membershipBillingRecoveryControllerProof) {
			p.Runtimes[0].ImageID = "sha256:" + strings.Repeat("f", 64)
		}},
		{"rollback-v20", func(p *membershipBillingRecoveryControllerProof) { p.Runtimes[1].SchemaContract.RuntimeMaxVersion = 20 }},
		{"writer-v20", func(p *membershipBillingRecoveryControllerProof) { p.Runtimes[2].SchemaContract.RuntimeMaxVersion = 20 }},
		{"unreviewed-max22", func(p *membershipBillingRecoveryControllerProof) { p.Runtimes[1].SchemaContract.RuntimeMaxVersion = 22 }},
		{"target-prefix", func(p *membershipBillingRecoveryControllerProof) {
			p.Runtimes[1].SchemaContract.CompatiblePrefixes["21"] = "other"
		}},
		{"runtime-management", func(p *membershipBillingRecoveryControllerProof) {
			p.Runtimes[1].SchemaContract.SchemaManagement = "runtime"
		}},
		{"missing-routing-evidence", func(p *membershipBillingRecoveryControllerProof) { p.Runtimes[1].TenantRoutingEnabled = nil }},
		{"tenant-unaware", func(p *membershipBillingRecoveryControllerProof) { *p.Runtimes[1].TenantAware = false }},
		{"separate-log-db", func(p *membershipBillingRecoveryControllerProof) { *p.Runtimes[1].SeparateLogDatabase = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			proof, status, now := membershipBillingRecoveryProofFixture(t)
			test.edit(proof)
			assert.Error(t, validateMembershipBillingRecoveryControllerProof(proof, status, now))
		})
	}
	for _, test := range []struct {
		name string
		edit func(*membershipBillingRecoveryMigrationStatus)
	}{
		{"status-schema", func(s *membershipBillingRecoveryMigrationStatus) { s.Schema = "other" }},
		{"noncanonical-source", func(s *membershipBillingRecoveryMigrationStatus) { s.CurrentVersion = 17 }},
		{"unreceipted-objects", func(s *membershipBillingRecoveryMigrationStatus) { s.MembershipBillingRecoverySchemaPresent = true }},
		{"incomplete-target", func(s *membershipBillingRecoveryMigrationStatus) { s.CurrentVersion = 21 }},
		{"unsafe-precheck", func(s *membershipBillingRecoveryMigrationStatus) { s.Safe = false }},
		{"status-target", func(s *membershipBillingRecoveryMigrationStatus) { s.TargetVersion = 22 }},
		{"status-source-digest", func(s *membershipBillingRecoveryMigrationStatus) { s.SourceDigest = "other" }},
		{"status-target-digest", func(s *membershipBillingRecoveryMigrationStatus) { s.TargetDigest = "other" }},
		{"status-plan", func(s *membershipBillingRecoveryMigrationStatus) { s.PlanChecksum = "other" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			proof, status, now := membershipBillingRecoveryProofFixture(t)
			test.edit(status)
			assert.Error(t, validateMembershipBillingRecoveryControllerProof(proof, status, now))
		})
	}
}

func TestMembershipBillingRecoveryCommandRequiresItsOwnFixedProof(t *testing.T) {
	for _, operation := range []string{"plan", "status"} {
		require.NoError(t, validateMembershipBillingRecoveryMigrationCommand(operation, "", true, false))
		assert.Error(t, validateMembershipBillingRecoveryMigrationCommand(operation, membershipBillingRecoveryControllerProofPath, true, false))
	}
	require.NoError(t, validateMembershipBillingRecoveryMigrationCommand("apply", membershipBillingRecoveryControllerProofPath, true, false))
	for _, path := range []string{"", "/tmp/proof.json", tenantControllerProofPath, invoiceControllerProofPath, walletPaymentControllerProofPath, shopControllerProofPath, membershipPaymentControllerProofPath} {
		assert.Error(t, validateMembershipBillingRecoveryMigrationCommand("apply", path, true, false))
	}
	assert.Error(t, validateMembershipBillingRecoveryMigrationCommand("apply", membershipBillingRecoveryControllerProofPath, false, false))
	assert.Error(t, validateMembershipBillingRecoveryMigrationCommand("apply", membershipBillingRecoveryControllerProofPath, true, true))
	assert.Error(t, validateMembershipBillingRecoveryMigrationCommand("repair", "", true, false))
}

func TestMembershipBillingRecoveryInspectionIsReadonlyAndRejectsUnreceiptedObjects(t *testing.T) {
	db := newMembershipBillingRecoveryCommandFixture(t)
	ctx := context.Background()
	require.NoError(t, db.Exec("PRAGMA query_only=ON").Error)
	for _, operation := range []string{"plan", "status"} {
		var output bytes.Buffer
		require.NoError(t, runMembershipBillingRecoveryMigration(ctx, db, operation, "", &output))
		var status membershipBillingRecoveryMigrationStatus
		require.NoError(t, common.Unmarshal(output.Bytes(), &status))
		assert.EqualValues(t, 20, status.CurrentVersion)
		assert.EqualValues(t, 21, status.TargetVersion)
		assert.False(t, status.MembershipBillingRecoverySchemaPresent)
		assert.True(t, status.Safe)
	}
	var output bytes.Buffer
	assert.Error(t, runMembershipBillingRecoveryMigration(ctx, db, "apply", membershipBillingRecoveryControllerProofPath, &output), "production mutation has no SQLite fallback")
	assert.False(t, db.Migrator().HasTable("kkai_membership_billing_recovery"))
	require.NoError(t, db.Exec("PRAGMA query_only=OFF").Error)
	tx := db.Begin()
	require.NoError(t, tx.Error)
	require.NoError(t, tx.Exec("CREATE TABLE kkai_membership_billing_recovery(request_id VARCHAR(128) PRIMARY KEY)").Error)
	_, err := inspectMembershipBillingRecoveryMigration(ctx, tx)
	assert.ErrorIs(t, err, kkaimigrate.ErrSchemaNotReady)
	require.NoError(t, tx.Rollback().Error)
	_, err = kkaimigrate.ApplyMembershipBillingRecoveryExpand(ctx, db, kkaimigrate.Options{Offline: true})
	require.NoError(t, err)
	require.NoError(t, db.Exec("PRAGMA query_only=ON").Error)
	status, err := inspectMembershipBillingRecoveryMigration(ctx, db)
	require.NoError(t, err)
	assert.EqualValues(t, 21, status.CurrentVersion)
	assert.True(t, status.MembershipBillingRecoverySchemaPresent)
	require.NoError(t, db.Exec("PRAGMA query_only=OFF").Error)
	require.NoError(t, db.Where("version = ?", 21).Delete(&kkaimigrate.AppliedMigration{}).Error)
	_, err = inspectMembershipBillingRecoveryMigration(ctx, db)
	assert.ErrorIs(t, err, kkaimigrate.ErrSchemaNotReady, "DDL without its canonical receipt never authorizes recovery")
}
