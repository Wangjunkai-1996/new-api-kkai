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

func tenantControllerProofFixture(operation string, now time.Time) (*tenantControllerProof, *kkaimigrate.TenantOnlineExpansionStatus) {
	status := &kkaimigrate.TenantOnlineExpansionStatus{PlanChecksum: kkaimigrate.TenantOnlinePlanChecksum(), SourceDigest: "sha256:" + strings.Repeat("a", 64), DatabaseName: "kkai", DatabaseSchema: "public"}
	proof := &tenantControllerProof{
		Schema: "kkai_tenant_online_proof_v1", Protocol: "tenant-online-v1", Operation: operation,
		InstalledInfraSHA: strings.Repeat("b", 40), ImageID: "sha256:" + strings.Repeat("c", 64),
		PlanChecksum: status.PlanChecksum, SourceDigest: status.SourceDigest, SourceVersion: 11, TargetVersion: 14,
		DatabaseName: status.DatabaseName, DatabaseSchema: status.DatabaseSchema,
		IssuedAt: now.Unix() - 1, ExpiresAt: now.Unix() + 119, Nonce: strings.Repeat("d", 32),
	}
	for i, role := range []string{"active", "rollback", "writer"} {
		routing, aware, logs := false, true, false
		proof.Runtimes = append(proof.Runtimes, tenantRuntimeEvidence{
			Name: role, Role: role, ContainerID: strings.Repeat(string(rune('a'+i)), 64),
			ImageID: proof.ImageID, Version: "test-release", TenantRoutingEnabled: &routing,
			TenantAware: &aware, SeparateLogDatabase: &logs,
		})
	}
	return proof, status
}

func TestTenantOnlineControllerProofBindsExactPlanDatabaseAndRuntimeState(t *testing.T) {
	now := time.Unix(1800000000, 0)
	for _, operation := range []string{"apply", "finalize"} {
		t.Run(operation, func(t *testing.T) {
			proof, status := tenantControllerProofFixture(operation, now)
			require.NoError(t, validateTenantControllerProof(proof, operation, status, now))
			*proof.Runtimes[0].TenantAware = false
			if operation == "apply" {
				require.NoError(t, validateTenantControllerProof(proof, operation, status, now), "additive expansion permits the inspected legacy binary")
			} else {
				assert.Error(t, validateTenantControllerProof(proof, operation, status, now), "the contract must retain only tenant-aware rollback binaries")
			}
		})
	}
	cases := []struct {
		name   string
		change func(*tenantControllerProof)
	}{
		{"wrong-operation", func(p *tenantControllerProof) { p.Operation = "apply" }},
		{"wrong-protocol", func(p *tenantControllerProof) { p.Protocol = "tenant-maintenance-v1" }},
		{"wrong-plan", func(p *tenantControllerProof) { p.PlanChecksum = strings.Repeat("e", 64) }},
		{"wrong-source", func(p *tenantControllerProof) { p.SourceDigest = "sha256:" + strings.Repeat("e", 64) }},
		{"wrong-source-version", func(p *tenantControllerProof) { p.SourceVersion = 13 }},
		{"wrong-target", func(p *tenantControllerProof) { p.TargetVersion = 13 }},
		{"wrong-database", func(p *tenantControllerProof) { p.DatabaseName = "other" }},
		{"wrong-schema", func(p *tenantControllerProof) { p.DatabaseSchema = "other" }},
		{"bad-image", func(p *tenantControllerProof) { p.ImageID = "latest" }},
		{"bad-infra-sha", func(p *tenantControllerProof) { p.InstalledInfraSHA = strings.Repeat("B", 40) }},
		{"bad-nonce", func(p *tenantControllerProof) { p.Nonce = "nonce" }},
		{"expired", func(p *tenantControllerProof) { p.ExpiresAt = now.Unix() }},
		{"future-issued", func(p *tenantControllerProof) { p.IssuedAt = now.Unix() + 1 }},
		{"too-long", func(p *tenantControllerProof) { p.ExpiresAt++ }},
		{"invalid-issued", func(p *tenantControllerProof) { p.IssuedAt = -1 }},
		{"missing-role", func(p *tenantControllerProof) { p.Runtimes = p.Runtimes[:2] }},
		{"duplicate-role", func(p *tenantControllerProof) { p.Runtimes[1].Role = "active" }},
		{"duplicate-container", func(p *tenantControllerProof) { p.Runtimes[1].ContainerID = p.Runtimes[0].ContainerID }},
		{"unbound-runtime-image", func(p *tenantControllerProof) { p.Runtimes[1].ImageID = "candidate" }},
		{"routes-enabled", func(p *tenantControllerProof) { *p.Runtimes[1].TenantRoutingEnabled = true }},
		{"missing-routing-state", func(p *tenantControllerProof) { p.Runtimes[1].TenantRoutingEnabled = nil }},
		{"missing-awareness", func(p *tenantControllerProof) { p.Runtimes[1].TenantAware = nil }},
		{"separate-log-db", func(p *tenantControllerProof) { *p.Runtimes[1].SeparateLogDatabase = true }},
		{"missing-log-db-state", func(p *tenantControllerProof) { p.Runtimes[1].SeparateLogDatabase = nil }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			proof, status := tenantControllerProofFixture("finalize", now)
			test.change(proof)
			assert.Error(t, validateTenantControllerProof(proof, "finalize", status, now))
		})
	}
}

func TestTenantOnlineControllerEvidenceRequiresTypedExplicitBooleans(t *testing.T) {
	now := time.Unix(1800000000, 0)
	for _, field := range []string{"tenant_routing_enabled", "tenant_aware", "separate_log_database"} {
		for _, value := range []string{"null", `"false"`} {
			t.Run(field+"="+value, func(t *testing.T) {
				proof, status := tenantControllerProofFixture("finalize", now)
				raw, err := common.Marshal(proof)
				require.NoError(t, err)
				old := `"` + field + `":false`
				if field == "tenant_aware" {
					old = `"` + field + `":true`
				}
				raw = bytes.Replace(raw, []byte(old), []byte(`"`+field+`":`+value), 1)
				var decoded tenantControllerProof
				err = common.Unmarshal(raw, &decoded)
				if value == "null" {
					require.NoError(t, err)
					assert.Error(t, validateTenantControllerProof(&decoded, "finalize", status, now))
				} else {
					assert.Error(t, err)
				}
			})
		}
	}
}

func TestTenantOnlineControllerWritesRequireFixedProofAndRejectOtherDatabases(t *testing.T) {
	for _, operation := range []string{"apply", "finalize"} {
		require.NoError(t, validateTenantOnlineCommand(operation, tenantControllerProofPath, true, false))
		for _, path := range []string{"", "/tmp/proof.json"} {
			assert.Error(t, validateTenantOnlineCommand(operation, path, true, false))
		}
		assert.Error(t, validateTenantOnlineCommand(operation, tenantControllerProofPath, false, false))
		assert.Error(t, validateTenantOnlineCommand(operation, tenantControllerProofPath, true, true))
	}
	assert.Error(t, validateTenantOnlineCommand("status", tenantControllerProofPath, true, false))
	_, err := readTenantControllerProof("/tmp/proof.json")
	assert.Error(t, err)
	assert.Error(t, runTenantControllerMutation(context.Background(), nil, "apply", tenantControllerProofPath, &bytes.Buffer{}))
	db, err := openDatabase("file:" + t.TempDir() + "/unsupported-online.db")
	require.NoError(t, err)
	assert.Error(t, runTenantControllerMutation(context.Background(), db, "apply", tenantControllerProofPath, &bytes.Buffer{}))
	assert.False(t, db.Migrator().HasTable("kkai_tenant_online_steps"))
}
