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

func requestTicketProofFixture(t *testing.T) (*requestTicketControllerProof, *requestTicketStatus, time.Time) {
	t.Helper()
	contract, err := kkaimigrate.ContractForDialect(kkaimigrate.DialectPostgres)
	require.NoError(t, err)
	// Preserve the installed historical controller's exact v14-v16 proof.
	// Declaring invoice compatibility does not authorize that controller to
	// mutate a newer schema or accept a broader runtime contract.
	contract.RuntimeMaxVersion = 16
	delete(contract.CompatiblePrefixes, "17")
	delete(contract.CompatiblePrefixes, "18")
	delete(contract.CompatiblePrefixes, "19")
	now := time.Unix(1800000000, 0)
	status := &requestTicketStatus{
		Schema: "kkai_request_ticket_v1", CurrentVersion: 15, TargetVersion: 16, Safe: true,
		SourceDigest: contract.CompatiblePrefixes["15"], TargetDigest: contract.CompatiblePrefixes["16"],
		PlanChecksum: kkaimigrate.RequestTicketPlanChecksum(), DatabaseName: "newapi", DatabaseSchema: "public",
	}
	proof := &requestTicketControllerProof{tenantControllerProof: tenantControllerProof{
		Schema: "kkai_request_ticket_proof_v1", Protocol: "request-ticket-v1", Operation: "apply",
		InstalledInfraSHA: strings.Repeat("a", 40), ImageID: "sha256:" + strings.Repeat("b", 64),
		SourceVersion: 15, TargetVersion: 16, SourceDigest: status.SourceDigest, PlanChecksum: status.PlanChecksum,
		DatabaseName: status.DatabaseName, DatabaseSchema: status.DatabaseSchema, IssuedAt: now.Unix() - 1,
		ExpiresAt: now.Unix() + 119, Nonce: strings.Repeat("c", 32),
	}, TargetDigest: status.TargetDigest}
	for i, role := range []string{"active", "rollback", "writer"} {
		aware, routing, logs := true, true, false
		evidence := requestTicketRuntimeEvidence{tenantRuntimeEvidence: tenantRuntimeEvidence{
			Name: role, Role: role, ContainerID: strings.Repeat(string(rune('a'+i)), 64), ImageID: proof.ImageID,
			Version: "release", TenantRoutingEnabled: &routing, TenantAware: &aware, SeparateLogDatabase: &logs,
		}}
		// Each running process declares its own immutable contract.
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

func TestRequestTicketProofBindsDatabasePlanEveryRuntimeAndActiveImage(t *testing.T) {
	proof, status, now := requestTicketProofFixture(t)
	require.NoError(t, validateRequestTicketProof(proof, status, now))
	encoded, err := common.Marshal(proof)
	require.NoError(t, err)
	var decoded requestTicketControllerProof
	require.NoError(t, common.Unmarshal(encoded, &decoded))
	require.Len(t, decoded.Runtimes, 3)
	require.NoError(t, validateRequestTicketProof(&decoded, status, now))
	status.CurrentVersion, status.TicketsPresent, status.EventsPresent = 16, true, true
	require.NoError(t, validateRequestTicketProof(proof, status, now), "an exact completed receipt is idempotently inspectable")

	for _, test := range []struct {
		name string
		edit func(*requestTicketControllerProof)
	}{
		{"historical-schema", func(p *requestTicketControllerProof) { p.Schema = "kkai_reseller_prefix_proof_v1" }},
		{"historical-protocol", func(p *requestTicketControllerProof) { p.Protocol = "reseller-prefix-v1" }},
		{"read-only-operation", func(p *requestTicketControllerProof) { p.Operation = "status" }},
		{"wrong-source-version", func(p *requestTicketControllerProof) { p.SourceVersion = 14 }},
		{"wrong-target-version", func(p *requestTicketControllerProof) { p.TargetVersion = 17 }},
		{"source-drift", func(p *requestTicketControllerProof) { p.SourceDigest = "other" }},
		{"target-drift", func(p *requestTicketControllerProof) { p.TargetDigest = "other" }},
		{"plan-drift", func(p *requestTicketControllerProof) { p.PlanChecksum = strings.Repeat("e", 64) }},
		{"database-drift", func(p *requestTicketControllerProof) { p.DatabaseName = "other" }},
		{"schema-drift", func(p *requestTicketControllerProof) { p.DatabaseSchema = "other" }},
		{"infra-identity", func(p *requestTicketControllerProof) { p.InstalledInfraSHA = strings.Repeat("A", 40) }},
		{"mutable-image", func(p *requestTicketControllerProof) { p.ImageID = "latest" }},
		{"bad-nonce", func(p *requestTicketControllerProof) { p.Nonce = "nonce" }},
		{"expired", func(p *requestTicketControllerProof) { p.ExpiresAt = now.Unix() }},
		{"future-issued", func(p *requestTicketControllerProof) { p.IssuedAt = now.Unix() + 1 }},
		{"zero-issued", func(p *requestTicketControllerProof) { p.IssuedAt = 0 }},
		{"overlong-validity", func(p *requestTicketControllerProof) { p.ExpiresAt++ }},
		{"missing-role", func(p *requestTicketControllerProof) { p.Runtimes = p.Runtimes[:2] }},
		{"unknown-role", func(p *requestTicketControllerProof) { p.Runtimes[1].Role = "candidate" }},
		{"duplicate-role", func(p *requestTicketControllerProof) { p.Runtimes[1].Role = "active" }},
		{"duplicate-container", func(p *requestTicketControllerProof) { p.Runtimes[1].ContainerID = p.Runtimes[0].ContainerID }},
		{"mutable-runtime-image", func(p *requestTicketControllerProof) { p.Runtimes[1].ImageID = "latest" }},
		{"active-image-unbound", func(p *requestTicketControllerProof) { p.Runtimes[0].ImageID = "sha256:" + strings.Repeat("f", 64) }},
		{"old-rollback", func(p *requestTicketControllerProof) { p.Runtimes[1].SchemaContract.RuntimeMaxVersion = 15 }},
		{"old-writer", func(p *requestTicketControllerProof) { p.Runtimes[2].SchemaContract.RuntimeMaxVersion = 15 }},
		{"higher-unreviewed-contract", func(p *requestTicketControllerProof) { p.Runtimes[1].SchemaContract.RuntimeMaxVersion = 17 }},
		{"migration-target-drift", func(p *requestTicketControllerProof) { p.Runtimes[1].SchemaContract.MigrationTargetVersion = 16 }},
		{"missing-prefix", func(p *requestTicketControllerProof) { delete(p.Runtimes[1].SchemaContract.CompatiblePrefixes, "16") }},
		{"prefix-digest-drift", func(p *requestTicketControllerProof) { p.Runtimes[1].SchemaContract.CompatiblePrefixes["16"] = "other" }},
		{"extra-prefix", func(p *requestTicketControllerProof) { p.Runtimes[1].SchemaContract.CompatiblePrefixes["17"] = "other" }},
		{"runtime-schema-management", func(p *requestTicketControllerProof) { p.Runtimes[1].SchemaContract.SchemaManagement = "runtime" }},
		{"missing-routing", func(p *requestTicketControllerProof) { p.Runtimes[1].TenantRoutingEnabled = nil }},
		{"missing-awareness", func(p *requestTicketControllerProof) { p.Runtimes[1].TenantAware = nil }},
		{"legacy-runtime", func(p *requestTicketControllerProof) { *p.Runtimes[1].TenantAware = false }},
		{"separate-log-db", func(p *requestTicketControllerProof) { *p.Runtimes[1].SeparateLogDatabase = true }},
		{"missing-log-db", func(p *requestTicketControllerProof) { p.Runtimes[1].SeparateLogDatabase = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			proof, status, now := requestTicketProofFixture(t)
			test.edit(proof)
			assert.Error(t, validateRequestTicketProof(proof, status, now))
		})
	}
	for _, test := range []struct {
		name string
		edit func(*requestTicketStatus)
	}{
		{"noncanonical-source", func(s *requestTicketStatus) { s.CurrentVersion = 14 }},
		{"source-table-present", func(s *requestTicketStatus) { s.TicketsPresent = true }},
		{"target-incomplete", func(s *requestTicketStatus) { s.CurrentVersion = 16 }},
		{"unsafe", func(s *requestTicketStatus) { s.Safe = false }},
		{"compiled-plan-drift", func(s *requestTicketStatus) { s.PlanChecksum = "other" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			proof, status, now := requestTicketProofFixture(t)
			test.edit(status)
			assert.Error(t, validateRequestTicketProof(proof, status, now))
		})
	}
}

func TestRequestTicketCommandRequiresInstalledProofOnlyForApply(t *testing.T) {
	for _, operation := range []string{"plan", "status"} {
		require.NoError(t, validateRequestTicketCommand(operation, "", true, false))
		assert.Error(t, validateRequestTicketCommand(operation, tenantControllerProofPath, true, false))
	}
	require.NoError(t, validateRequestTicketCommand("apply", tenantControllerProofPath, true, false))
	for _, path := range []string{"", "/tmp/proof.json"} {
		assert.Error(t, validateRequestTicketCommand("apply", path, true, false))
	}
	assert.Error(t, validateRequestTicketCommand("apply", tenantControllerProofPath, false, false))
	assert.Error(t, validateRequestTicketCommand("apply", tenantControllerProofPath, true, true))
	assert.Error(t, validateRequestTicketCommand("repair", "", true, false))
	assert.Error(t, runRequestTicket(context.Background(), nil, "apply", tenantControllerProofPath, &bytes.Buffer{}))
}

func TestRequestTicketInspectionIsReadOnlyAndRefusesUnreceiptedTablesAndDrift(t *testing.T) {
	db, err := openDatabase("file:" + t.TempDir() + "/tickets.db")
	require.NoError(t, err)
	prepareLegacyAuthenticationTables(t, db)
	_, err = applyHistoricalFixture(context.Background(), db)
	require.NoError(t, err)
	for _, statement := range []string{
		"ALTER TABLE users ADD COLUMN username VARCHAR(20)", "ALTER TABLE users ADD COLUMN email VARCHAR(50)",
		"CREATE UNIQUE INDEX users_username_key ON users(username)", "CREATE TABLE top_ups(id BIGINT PRIMARY KEY)",
		"CREATE TABLE user_oauth_bindings(id BIGINT PRIMARY KEY)", "CREATE TABLE logs(id BIGINT PRIMARY KEY)",
		"CREATE TABLE quota_data(id BIGINT PRIMARY KEY)", "CREATE TABLE tasks(id BIGINT PRIMARY KEY)",
	} {
		require.NoError(t, db.Exec(statement).Error)
	}
	ctx := context.Background()
	_, err = kkaimigrate.ApplyTenantOnlineExpansion(ctx, db, kkaimigrate.Options{})
	require.NoError(t, err)
	_, err = kkaimigrate.FinalizeTenantOnlineSchema(ctx, db, kkaimigrate.Options{})
	require.NoError(t, err)
	_, err = kkaimigrate.ApplyResellerApplicationExpand(ctx, db, kkaimigrate.Options{})
	require.NoError(t, err)
	require.NoError(t, db.Exec("PRAGMA query_only=ON").Error)
	var output bytes.Buffer
	require.NoError(t, runRequestTicket(ctx, db, "plan", "", &output))
	var status requestTicketStatus
	require.NoError(t, common.Unmarshal(output.Bytes(), &status))
	assert.EqualValues(t, 15, status.CurrentVersion)
	assert.False(t, status.TicketsPresent)
	assert.False(t, status.EventsPresent)
	assert.NotEmpty(t, status.SourceDigest)
	assert.NotEmpty(t, status.TargetDigest)
	assert.Equal(t, kkaimigrate.RequestTicketPlanChecksum(), status.PlanChecksum)
	assert.Error(t, runRequestTicket(ctx, db, "apply", tenantControllerProofPath, &output), "production mutation has no SQLite fallback")
	assert.False(t, db.Migrator().HasTable("request_tickets"))
	assert.False(t, db.Migrator().HasTable("request_ticket_events"))
	require.NoError(t, db.Exec("PRAGMA query_only=OFF").Error)
	_, err = kkaimigrate.ApplyRequestTicketExpand(ctx, db, kkaimigrate.Options{})
	require.NoError(t, err)
	var receipt kkaimigrate.AppliedMigration
	require.NoError(t, db.Where("version = ?", 16).Take(&receipt).Error)
	require.NoError(t, db.Where("version = ?", 16).Delete(&receipt).Error)
	_, err = inspectRequestTicket(ctx, db)
	assert.ErrorIs(t, err, kkaimigrate.ErrSchemaNotReady, "tables alone are never an owned migration")
	require.NoError(t, db.Create(&receipt).Error)
	require.NoError(t, db.Exec("PRAGMA query_only=ON").Error)
	output.Reset()
	require.NoError(t, runRequestTicket(ctx, db, "status", "", &output))
	require.NoError(t, common.Unmarshal(output.Bytes(), &status))
	assert.EqualValues(t, 16, status.CurrentVersion)
	assert.True(t, status.TicketsPresent)
	assert.True(t, status.EventsPresent)
	require.NoError(t, db.Exec("PRAGMA query_only=OFF").Error)
	require.NoError(t, db.Exec("DROP INDEX idx_request_ticket_events_unread").Error)
	_, err = inspectRequestTicket(ctx, db)
	assert.ErrorIs(t, err, kkaimigrate.ErrSchemaNotReady, "receipt does not hide physical schema drift")
}
