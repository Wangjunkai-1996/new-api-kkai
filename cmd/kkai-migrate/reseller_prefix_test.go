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

func resellerPrefixProofFixture(t *testing.T) (*resellerPrefixControllerProof, *resellerPrefixStatus, time.Time) {
	t.Helper()
	contract, err := kkaimigrate.ContractForDialect(kkaimigrate.DialectPostgres)
	require.NoError(t, err)
	now := time.Unix(1800000000, 0)
	status := &resellerPrefixStatus{SourceDigest: contract.CompatiblePrefixes["14"], TargetDigest: contract.CompatiblePrefixes["15"],
		PlanChecksum: kkaimigrate.ResellerApplicationPlanChecksum(), DatabaseName: "newapi", DatabaseSchema: "public"}
	proof := &resellerPrefixControllerProof{tenantControllerProof: tenantControllerProof{
		Schema: "kkai_reseller_prefix_proof_v1", Protocol: "reseller-prefix-v1", Operation: "apply",
		InstalledInfraSHA: strings.Repeat("a", 40), ImageID: "sha256:" + strings.Repeat("b", 64),
		SourceVersion: 14, TargetVersion: 15, SourceDigest: status.SourceDigest, PlanChecksum: status.PlanChecksum,
		DatabaseName: status.DatabaseName, DatabaseSchema: status.DatabaseSchema, IssuedAt: now.Unix() - 1,
		ExpiresAt: now.Unix() + 119, Nonce: strings.Repeat("c", 32),
	}, TargetDigest: status.TargetDigest}
	for i, role := range []string{"active", "rollback", "writer"} {
		aware, routing, logs := true, true, false
		evidence := resellerPrefixRuntimeEvidence{tenantRuntimeEvidence: tenantRuntimeEvidence{
			Name: role, Role: role, ContainerID: strings.Repeat(string(rune('a'+i)), 64), ImageID: proof.ImageID,
			Version: "release", TenantRoutingEnabled: &routing, TenantAware: &aware, SeparateLogDatabase: &logs,
		}}
		evidence.SchemaContract.SchemaContract, evidence.SchemaContract.SchemaManagement = contract, "external"
		proof.Runtimes = append(proof.Runtimes, evidence)
	}
	return proof, status, now
}

func TestResellerPrefixProofRequiresEveryRuntimeToAcceptExactTarget(t *testing.T) {
	proof, status, now := resellerPrefixProofFixture(t)
	require.NoError(t, validateResellerPrefixProof(proof, status, now))
	for _, test := range []struct {
		name string
		edit func(*resellerPrefixControllerProof)
	}{
		{"old-runtime", func(p *resellerPrefixControllerProof) { p.Runtimes[1].SchemaContract.RuntimeMaxVersion = 14 }},
		{"unknown-target-digest", func(p *resellerPrefixControllerProof) {
			p.Runtimes[1].SchemaContract.CompatiblePrefixes = map[string]string{"14": p.SourceDigest, "15": "other"}
		}},
		{"runtime-schema-management", func(p *resellerPrefixControllerProof) { p.Runtimes[1].SchemaContract.SchemaManagement = "runtime" }},
		{"source-drift", func(p *resellerPrefixControllerProof) { p.SourceDigest = "other" }},
		{"target-drift", func(p *resellerPrefixControllerProof) { p.TargetDigest = "other" }},
		{"database-drift", func(p *resellerPrefixControllerProof) { p.DatabaseName = "other" }},
		{"expired", func(p *resellerPrefixControllerProof) { p.ExpiresAt = now.Unix() }},
		{"historical-protocol", func(p *resellerPrefixControllerProof) { p.Protocol = "tenant-online-v1" }},
		{"duplicate-role", func(p *resellerPrefixControllerProof) { p.Runtimes[1].Role = "active" }},
		{"duplicate-container", func(p *resellerPrefixControllerProof) { p.Runtimes[1].ContainerID = p.Runtimes[0].ContainerID }},
		{"missing-routing", func(p *resellerPrefixControllerProof) { p.Runtimes[1].TenantRoutingEnabled = nil }},
		{"separate-log-db", func(p *resellerPrefixControllerProof) { *p.Runtimes[1].SeparateLogDatabase = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			proof, status, now := resellerPrefixProofFixture(t)
			test.edit(proof)
			assert.Error(t, validateResellerPrefixProof(proof, status, now))
		})
	}
}

func TestResellerPrefixCommandNeverExposesUnguardedMutation(t *testing.T) {
	for _, operation := range []string{"plan", "status"} {
		require.NoError(t, validateResellerPrefixCommand(operation, "", true, false))
		assert.Error(t, validateResellerPrefixCommand(operation, tenantControllerProofPath, true, false))
	}
	require.NoError(t, validateResellerPrefixCommand("apply", tenantControllerProofPath, true, false))
	for _, path := range []string{"", "/tmp/proof.json"} {
		assert.Error(t, validateResellerPrefixCommand("apply", path, true, false))
	}
	assert.Error(t, validateResellerPrefixCommand("apply", tenantControllerProofPath, false, false))
	assert.Error(t, validateResellerPrefixCommand("apply", tenantControllerProofPath, true, true))
	assert.Error(t, validateResellerPrefixCommand("repair", "", true, false))
	_, err := applyMigrationTarget(context.Background(), nil, 15, kkaimigrate.Options{})
	assert.ErrorIs(t, err, kkaimigrate.ErrUnsafeMigration)
}

func TestResellerPrefixInspectionAndTenantCheckReadFinalV15(t *testing.T) {
	db, err := openDatabase("file:" + t.TempDir() + "/prefix.db")
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
	require.NoError(t, db.Exec("PRAGMA query_only=ON").Error)
	var output bytes.Buffer
	require.NoError(t, runResellerPrefix(ctx, db, "plan", "", &output))
	var status resellerPrefixStatus
	require.NoError(t, common.Unmarshal(output.Bytes(), &status))
	assert.EqualValues(t, 14, status.CurrentVersion)
	assert.False(t, status.ColumnPresent)
	assert.NotEmpty(t, status.TargetDigest)
	require.NoError(t, db.Exec("PRAGMA query_only=OFF").Error)
	assert.Error(t, runResellerPrefix(ctx, db, "apply", tenantControllerProofPath, &output), "no production mutation fallback on SQLite")
	assert.False(t, db.Migrator().HasColumn("reseller_applications", "desired_domain_prefix"))
	_, err = kkaimigrate.ApplyResellerApplicationExpand(ctx, db, kkaimigrate.Options{})
	require.NoError(t, err)
	require.NoError(t, db.Exec("PRAGMA query_only=ON").Error)
	output.Reset()
	require.NoError(t, runResellerPrefix(ctx, db, "status", "", &output))
	require.NoError(t, common.Unmarshal(output.Bytes(), &status))
	assert.EqualValues(t, 15, status.CurrentVersion)
	assert.True(t, status.ColumnPresent)
	output.Reset()
	require.NoError(t, runTenantOnlineInspection(ctx, db, "check", &output))
}
