package main

import (
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func rc49ProofFixture(t *testing.T) (*rc49ControllerProof, *kkaimigrate.RC49OnlineStatus, time.Time) {
	t.Helper()
	now := time.Unix(1800000000, 0)
	status := &kkaimigrate.RC49OnlineStatus{Schema: "kkai_rc49_expansion_status_v1", CurrentVersion: 21, TargetVersion: 21,
		SourceDigest: kkaimigrate.RC49OnlineSourceDigest(), TargetDigest: kkaimigrate.RC49OnlineSourceDigest(),
		PlanChecksum: kkaimigrate.RC49OnlinePlanChecksum(), ExpansionDigest: kkaimigrate.RC49OnlineExpansionDigest(),
		DatabaseName: "newapi", DatabaseSchema: "public", State: "pristine", Safe: true}
	proof := &rc49ControllerProof{tenantControllerProof: tenantControllerProof{Schema: "kkai_rc49_expansion_proof_v1", Protocol: "rc49-expansion-v1", Operation: "apply",
		SourceVersion: 21, TargetVersion: 21, SourceDigest: status.SourceDigest, PlanChecksum: status.PlanChecksum,
		InstalledInfraSHA: strings.Repeat("a", 40), ImageID: "sha256:" + strings.Repeat("b", 64), Nonce: strings.Repeat("c", 32),
		DatabaseName: "newapi", DatabaseSchema: "public", IssuedAt: now.Unix() - 1, ExpiresAt: now.Unix() + 119},
		TargetDigest: status.TargetDigest, ExpansionDigest: status.ExpansionDigest}
	contract, err := kkaimigrate.ContractForDialect(kkaimigrate.DialectPostgres)
	require.NoError(t, err)
	for i, role := range []string{"active", "rollback", "writer"} {
		aware, routing, logs := true, true, false
		instance := shopRuntimeEvidence{tenantRuntimeEvidence: tenantRuntimeEvidence{
			Name: role, Role: role, ContainerID: strings.Repeat(string(rune('a'+i)), 64), ImageID: "sha256:" + strings.Repeat("d", 64),
			Version: "accepted-old-release", TenantAware: &aware, TenantRoutingEnabled: &routing, SeparateLogDatabase: &logs},
			ContainerStartedAt: now.Add(-time.Minute).Format(time.RFC3339Nano), ContainerInitStartTicks: 100,
			ApplicationPID: int64(10 + i), ApplicationStartTicks: 101, ApplicationMountNamespace: "mnt:[123]",
			ApplicationExecutableDevice: 1, ApplicationExecutableInode: 2, ApplicationCgroupSHA256: strings.Repeat("e", 64)}
		instance.SchemaContract.SchemaContract, instance.SchemaContract.SchemaManagement = contract, "external"
		proof.Runtimes = append(proof.Runtimes, instance)
	}
	return proof, status, now
}

func TestRC49ControllerProofBindsPlanDatabaseAndEveryProcess(t *testing.T) {
	proof, status, now := rc49ProofFixture(t)
	require.NoError(t, validateRC49ControllerProof(proof, "apply", status, now), "the reviewed preparation artifact may differ from the active image")
	raw, err := common.Marshal(proof)
	require.NoError(t, err)
	for _, test := range []struct {
		name string
		edit func(*rc49ControllerProof)
	}{
		{"legacy protocol", func(p *rc49ControllerProof) { p.Protocol = "membership-billing-recovery-migration-v1" }},
		{"canonical22", func(p *rc49ControllerProof) { p.TargetVersion = 22 }},
		{"wrong digest", func(p *rc49ControllerProof) { p.SourceDigest = "wrong" }},
		{"wrong expansion", func(p *rc49ControllerProof) { p.ExpansionDigest = "wrong" }},
		{"wrong plan", func(p *rc49ControllerProof) { p.PlanChecksum = "wrong" }},
		{"wrong database", func(p *rc49ControllerProof) { p.DatabaseName = "other" }},
		{"wrong schema", func(p *rc49ControllerProof) { p.DatabaseSchema = "other" }},
		{"expired", func(p *rc49ControllerProof) { p.ExpiresAt = now.Unix() }},
		{"overlong", func(p *rc49ControllerProof) { p.ExpiresAt++ }},
		{"future", func(p *rc49ControllerProof) { p.IssuedAt = now.Unix() + 1 }},
		{"mutable artifact", func(p *rc49ControllerProof) { p.ImageID = "latest" }},
		{"missing writer", func(p *rc49ControllerProof) { p.Runtimes = p.Runtimes[:2] }},
		{"duplicate role", func(p *rc49ControllerProof) { p.Runtimes[1].Role = "active" }},
		{"duplicate process", func(p *rc49ControllerProof) { p.Runtimes[1].ApplicationPID = p.Runtimes[0].ApplicationPID }},
		{"process start drift", func(p *rc49ControllerProof) { p.Runtimes[1].ApplicationStartTicks = 99 }},
		{"container drift", func(p *rc49ControllerProof) { p.Runtimes[1].ContainerID = "wrong" }},
		{"namespace drift", func(p *rc49ControllerProof) { p.Runtimes[1].ApplicationMountNamespace = "wrong" }},
		{"executable drift", func(p *rc49ControllerProof) { p.Runtimes[1].ApplicationExecutableInode = 0 }},
		{"cgroup drift", func(p *rc49ControllerProof) { p.Runtimes[1].ApplicationCgroupSHA256 = "wrong" }},
		{"separate log DB", func(p *rc49ControllerProof) { *p.Runtimes[1].SeparateLogDatabase = true }},
		{"auto schema", func(p *rc49ControllerProof) { p.Runtimes[1].SchemaContract.SchemaManagement = "auto" }},
		{"prefix drift", func(p *rc49ControllerProof) { p.Runtimes[1].SchemaContract.CompatiblePrefixes["21"] = "wrong" }},
		{"expansion drift", func(p *rc49ControllerProof) {
			p.Runtimes[1].SchemaContract.RequiredExpansions[kkaimigrate.RC49OnlineExpansion] = "wrong"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var changed rc49ControllerProof
			require.NoError(t, common.Unmarshal(raw, &changed))
			test.edit(&changed)
			assert.Error(t, validateRC49ControllerProof(&changed, "apply", status, now))
		})
	}
}

func TestRC49RepairProofCannotAuthorizeApplyOrValidIndexRemoval(t *testing.T) {
	proof, status, now := rc49ProofFixture(t)
	proof.Operation = "repair-index"
	assert.Error(t, validateRC49ControllerProof(proof, "repair-index", status, now))
	status.State, status.Safe = "index_invalid", false
	require.NoError(t, validateRC49ControllerProof(proof, "repair-index", status, now))
	assert.Error(t, validateRC49ControllerProof(proof, "apply", status, now))
	proof.Operation = "apply"
	assert.Error(t, validateRC49ControllerProof(proof, "apply", status, now))
}

func TestRC49CommandRequiresIndependentProofAndExplicitOperation(t *testing.T) {
	for _, operation := range []string{"plan", "status"} {
		require.NoError(t, validateRC49OnlineCommand(operation, "", true, false))
		assert.Error(t, validateRC49OnlineCommand(operation, rc49ControllerProofPath, true, false))
	}
	for _, operation := range []string{"apply", "repair-index"} {
		require.NoError(t, validateRC49OnlineCommand(operation, rc49ControllerProofPath, true, false))
		assert.Error(t, validateRC49OnlineCommand(operation, "", true, false))
		assert.Error(t, validateRC49OnlineCommand(operation, membershipBillingRecoveryControllerProofPath, true, false))
		assert.Error(t, validateRC49OnlineCommand(operation, rc49ControllerProofPath, true, true))
	}
	assert.Error(t, validateRC49OnlineCommand("finalize", rc49ControllerProofPath, true, false))
	assert.Error(t, validateRC49OnlineCommand("plan", "", false, false))
}
