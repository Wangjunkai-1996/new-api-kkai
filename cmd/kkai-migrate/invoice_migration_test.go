//go:build !kkai_bridge

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func invoiceProofFixture(t *testing.T) (*invoiceControllerProof, *invoiceMigrationStatus, time.Time) {
	t.Helper()
	contract, err := kkaimigrate.ContractForDialect(kkaimigrate.DialectPostgres)
	require.NoError(t, err)
	now := time.Unix(1800000000, 0)
	status := &invoiceMigrationStatus{
		Schema: "kkai_invoice_migration_status_v1", CurrentVersion: 16, TargetVersion: 17, Safe: true,
		SourceDigest: contract.CompatiblePrefixes["16"], TargetDigest: contract.CompatiblePrefixes["17"],
		PlanChecksum: kkaimigrate.InvoicePlanChecksum(), DatabaseName: "newapi", DatabaseSchema: "public",
	}
	proof := &invoiceControllerProof{tenantControllerProof: tenantControllerProof{
		Schema: "kkai_invoice_migration_proof_v1", Protocol: "invoice-migration-v1", Operation: "apply",
		InstalledInfraSHA: strings.Repeat("a", 40), ImageID: "sha256:" + strings.Repeat("b", 64),
		SourceVersion: 16, TargetVersion: 17, SourceDigest: status.SourceDigest, PlanChecksum: status.PlanChecksum,
		DatabaseName: status.DatabaseName, DatabaseSchema: status.DatabaseSchema, IssuedAt: now.Unix() - 1,
		ExpiresAt: now.Unix() + 119, Nonce: strings.Repeat("c", 32),
	}, TargetDigest: status.TargetDigest}
	for i, role := range []string{"active", "rollback", "writer"} {
		aware, routing, logs := true, true, false
		evidence := invoiceRuntimeEvidence{
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

func TestInvoiceProofBindsDatabasePlanProcessAndEveryRuntime(t *testing.T) {
	proof, status, now := invoiceProofFixture(t)
	require.NoError(t, validateInvoiceControllerProof(proof, status, now))
	encoded, err := common.Marshal(proof)
	require.NoError(t, err)
	var decoded invoiceControllerProof
	require.NoError(t, common.Unmarshal(encoded, &decoded))
	require.Len(t, decoded.Runtimes, 3)
	require.NoError(t, validateInvoiceControllerProof(&decoded, status, now))
	status.CurrentVersion, status.InvoicesPresent = 17, true
	require.NoError(t, validateInvoiceControllerProof(proof, status, now), "the exact completed receipt is safe to inspect idempotently")
	assert.Error(t, validateInvoiceControllerProof(proof, status, now.Add(119*time.Second)), "expiry is rechecked at the mutation boundary")

	for _, test := range []struct {
		name string
		edit func(*invoiceControllerProof)
	}{
		{"historical-proof-schema", func(p *invoiceControllerProof) { p.Schema = "kkai_request_ticket_proof_v1" }},
		{"historical-protocol", func(p *invoiceControllerProof) { p.Protocol = "request-ticket-v1" }},
		{"wrong-operation", func(p *invoiceControllerProof) { p.Operation = "status" }},
		{"wrong-source-version", func(p *invoiceControllerProof) { p.SourceVersion = 15 }},
		{"wrong-target-version", func(p *invoiceControllerProof) { p.TargetVersion = 16 }},
		{"source-drift", func(p *invoiceControllerProof) { p.SourceDigest = "other" }},
		{"target-drift", func(p *invoiceControllerProof) { p.TargetDigest = "other" }},
		{"plan-drift", func(p *invoiceControllerProof) { p.PlanChecksum = "other" }},
		{"database-drift", func(p *invoiceControllerProof) { p.DatabaseName = "other" }},
		{"schema-drift", func(p *invoiceControllerProof) { p.DatabaseSchema = "other" }},
		{"infra-identity", func(p *invoiceControllerProof) { p.InstalledInfraSHA = "not-a-sha" }},
		{"mutable-image", func(p *invoiceControllerProof) { p.ImageID = "latest" }},
		{"invalid-nonce", func(p *invoiceControllerProof) { p.Nonce = "nonce" }},
		{"expired", func(p *invoiceControllerProof) { p.ExpiresAt = now.Unix() }},
		{"future-issued", func(p *invoiceControllerProof) { p.IssuedAt = now.Unix() + 1 }},
		{"overlong-validity", func(p *invoiceControllerProof) { p.ExpiresAt++ }},
		{"missing-role", func(p *invoiceControllerProof) { p.Runtimes = p.Runtimes[:2] }},
		{"duplicate-role", func(p *invoiceControllerProof) { p.Runtimes[1].Role = "active" }},
		{"duplicate-container", func(p *invoiceControllerProof) { p.Runtimes[1].ContainerID = p.Runtimes[0].ContainerID }},
		{"invalid-process-time", func(p *invoiceControllerProof) { p.Runtimes[1].ContainerStartedAt = "missing" }},
		{"future-container-time", func(p *invoiceControllerProof) {
			p.Runtimes[1].ContainerStartedAt = now.Add(time.Second).Format(time.RFC3339Nano)
		}},
		{"missing-container-start", func(p *invoiceControllerProof) { p.Runtimes[1].ContainerInitStartTicks = 0 }},
		{"missing-application-process", func(p *invoiceControllerProof) { p.Runtimes[1].ApplicationPID = 0 }},
		{"duplicate-application-process", func(p *invoiceControllerProof) { p.Runtimes[1].ApplicationPID = p.Runtimes[0].ApplicationPID }},
		{"missing-process-start", func(p *invoiceControllerProof) { p.Runtimes[1].ApplicationStartTicks = 0 }},
		{"process-before-container", func(p *invoiceControllerProof) { p.Runtimes[1].ApplicationStartTicks = 99 }},
		{"invalid-mount-namespace", func(p *invoiceControllerProof) { p.Runtimes[1].ApplicationMountNamespace = "missing" }},
		{"missing-executable-inode", func(p *invoiceControllerProof) { p.Runtimes[1].ApplicationExecutableInode = 0 }},
		{"missing-cgroup", func(p *invoiceControllerProof) { p.Runtimes[1].ApplicationCgroupSHA256 = "missing" }},
		{"mutable-runtime-image", func(p *invoiceControllerProof) { p.Runtimes[1].ImageID = "latest" }},
		{"unbound-active-image", func(p *invoiceControllerProof) { p.Runtimes[0].ImageID = "sha256:" + strings.Repeat("f", 64) }},
		{"incompatible-rollback", func(p *invoiceControllerProof) { p.Runtimes[1].SchemaContract.RuntimeMaxVersion = 16 }},
		{"incompatible-writer", func(p *invoiceControllerProof) { p.Runtimes[2].SchemaContract.RuntimeMaxVersion = 16 }},
		{"unreviewed-runtime", func(p *invoiceControllerProof) { p.Runtimes[1].SchemaContract.RuntimeMaxVersion = 18 }},
		{"runtime-prefix-drift", func(p *invoiceControllerProof) { p.Runtimes[1].SchemaContract.CompatiblePrefixes["17"] = "other" }},
		{"missing-runtime-prefix", func(p *invoiceControllerProof) { delete(p.Runtimes[1].SchemaContract.CompatiblePrefixes, "17") }},
		{"runtime-schema-management", func(p *invoiceControllerProof) { p.Runtimes[1].SchemaContract.SchemaManagement = "runtime" }},
		{"missing-routing-evidence", func(p *invoiceControllerProof) { p.Runtimes[1].TenantRoutingEnabled = nil }},
		{"legacy-runtime", func(p *invoiceControllerProof) { *p.Runtimes[1].TenantAware = false }},
		{"separate-log-db", func(p *invoiceControllerProof) { *p.Runtimes[1].SeparateLogDatabase = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			proof, status, now := invoiceProofFixture(t)
			test.edit(proof)
			assert.Error(t, validateInvoiceControllerProof(proof, status, now))
		})
	}
	for _, test := range []struct {
		name string
		edit func(*invoiceMigrationStatus)
	}{
		{"noncanonical-source", func(s *invoiceMigrationStatus) { s.CurrentVersion = 15 }},
		{"unreceipted-tables", func(s *invoiceMigrationStatus) { s.InvoicesPresent = true }},
		{"incomplete-target", func(s *invoiceMigrationStatus) { s.CurrentVersion = 17 }},
		{"unsafe", func(s *invoiceMigrationStatus) { s.Safe = false }},
		{"compiled-plan-drift", func(s *invoiceMigrationStatus) { s.PlanChecksum = "other" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			proof, status, now := invoiceProofFixture(t)
			test.edit(status)
			assert.Error(t, validateInvoiceControllerProof(proof, status, now))
		})
	}
}

func TestInvoiceCommandIsIsolatedAndRequiresInstalledProof(t *testing.T) {
	for _, operation := range []string{"plan", "status"} {
		require.NoError(t, validateInvoiceMigrationCommand(operation, "", true, false))
		assert.Error(t, validateInvoiceMigrationCommand(operation, invoiceControllerProofPath, true, false))
	}
	require.NoError(t, validateInvoiceMigrationCommand("apply", invoiceControllerProofPath, true, false))
	for _, path := range []string{"", "/tmp/proof.json", tenantControllerProofPath} {
		assert.Error(t, validateInvoiceMigrationCommand("apply", path, true, false))
	}
	assert.Error(t, validateInvoiceMigrationCommand("apply", invoiceControllerProofPath, false, false))
	assert.Error(t, validateInvoiceMigrationCommand("apply", invoiceControllerProofPath, true, true))
	assert.Error(t, validateInvoiceMigrationCommand("repair", "", true, false))
	assert.Error(t, runInvoiceMigration(context.Background(), nil, "apply", invoiceControllerProofPath, &bytes.Buffer{}))
	_, err := readRootControllerProofJSON(tenantControllerProofPath, invoiceControllerProofPath)
	assert.Error(t, err, "a historical root proof mount cannot authorize invoices")
}

func newInvoiceCommandFixture(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := openDatabase("file:" + t.TempDir() + "/invoices.db")
	require.NoError(t, err)
	prepareLegacyAuthenticationTables(t, db)
	ctx := context.Background()
	_, err = applyHistoricalFixture(ctx, db)
	require.NoError(t, err)
	for _, statement := range []string{
		"ALTER TABLE users ADD COLUMN username VARCHAR(20)", "ALTER TABLE users ADD COLUMN email VARCHAR(50)",
		"CREATE UNIQUE INDEX users_username_key ON users(username)", "CREATE TABLE top_ups(id BIGINT PRIMARY KEY)",
		"CREATE TABLE user_oauth_bindings(id BIGINT PRIMARY KEY)", "CREATE TABLE logs(id BIGINT PRIMARY KEY)",
		"CREATE TABLE quota_data(id BIGINT PRIMARY KEY)", "CREATE TABLE tasks(id BIGINT PRIMARY KEY)",
	} {
		require.NoError(t, db.Exec(statement).Error)
	}
	_, err = kkaimigrate.ApplyTenantOnlineExpansion(ctx, db, kkaimigrate.Options{})
	require.NoError(t, err)
	_, err = kkaimigrate.FinalizeTenantOnlineSchema(ctx, db, kkaimigrate.Options{})
	require.NoError(t, err)
	_, err = kkaimigrate.ApplyResellerApplicationExpand(ctx, db, kkaimigrate.Options{})
	require.NoError(t, err)
	_, err = kkaimigrate.ApplyRequestTicketExpand(ctx, db, kkaimigrate.Options{})
	require.NoError(t, err)
	return db
}

func TestInvoiceInspectionIsReadOnlyAndRequiresCompleteReceiptAndShape(t *testing.T) {
	db := newInvoiceCommandFixture(t)
	ctx := context.Background()
	require.NoError(t, db.Exec("PRAGMA query_only=ON").Error)
	var output bytes.Buffer
	require.NoError(t, runInvoiceMigration(ctx, db, "plan", "", &output))
	var status invoiceMigrationStatus
	require.NoError(t, common.Unmarshal(output.Bytes(), &status))
	assert.EqualValues(t, 16, status.CurrentVersion)
	assert.False(t, status.InvoicesPresent)
	assert.Equal(t, kkaimigrate.InvoicePlanChecksum(), status.PlanChecksum)
	assert.NotEmpty(t, status.SourceDigest)
	assert.NotEmpty(t, status.TargetDigest)
	assert.Error(t, runInvoiceMigration(ctx, db, "apply", invoiceControllerProofPath, &output), "production mutation has no SQLite fallback")
	assert.False(t, db.Migrator().HasTable("invoice_requests"))
	require.NoError(t, db.Exec("PRAGMA query_only=OFF").Error)
	_, err := kkaimigrate.ApplyInvoiceExpand(ctx, db, kkaimigrate.Options{Offline: true})
	require.NoError(t, err)
	var receipt kkaimigrate.AppliedMigration
	require.NoError(t, db.Where("version = ?", 17).Take(&receipt).Error)
	require.NoError(t, db.Where("version = ?", 17).Delete(&receipt).Error)
	_, err = inspectInvoiceMigration(ctx, db)
	assert.ErrorIs(t, err, kkaimigrate.ErrSchemaNotReady, "unreceipted tables cannot be adopted by production apply")
	require.NoError(t, db.Create(&receipt).Error)
	require.NoError(t, db.Exec("PRAGMA query_only=ON").Error)
	output.Reset()
	require.NoError(t, runInvoiceMigration(ctx, db, "status", "", &output))
	require.NoError(t, common.Unmarshal(output.Bytes(), &status))
	assert.EqualValues(t, 17, status.CurrentVersion)
	assert.True(t, status.InvoicesPresent)
	require.NoError(t, db.Exec("PRAGMA query_only=OFF").Error)
	require.NoError(t, db.Exec("DROP INDEX idx_invoice_claim").Error)
	_, err = inspectInvoiceMigration(ctx, db)
	assert.ErrorIs(t, err, kkaimigrate.ErrSchemaNotReady, "receipt cannot conceal lost financial uniqueness")
}

func TestInvoiceInspectionRefusesUnreceiptedPaymentReceiptTable(t *testing.T) {
	db := newInvoiceCommandFixture(t)
	require.NoError(t, db.Migrator().CreateTable(&model.InvoicePaymentReceipt{}))
	_, err := inspectInvoiceMigration(context.Background(), db)
	assert.ErrorIs(t, err, kkaimigrate.ErrSchemaNotReady, "a receipt-only partial expansion cannot be adopted by the production controller")
	assert.False(t, db.Migrator().HasTable("invoice_requests"))
}
