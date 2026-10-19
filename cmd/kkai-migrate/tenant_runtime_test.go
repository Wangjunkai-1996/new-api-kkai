package main

import (
	"bytes"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantOnlineRuntimeDeclarationReportsCompiledContractAndActualEnvironment(t *testing.T) {
	t.Setenv("TENANT_ROUTING_ENABLED", "false")
	t.Setenv("LOG_SQL_DSN", "")
	t.Setenv("KKAI_NODE_ROLE", "serving")
	var output bytes.Buffer
	require.NoError(t, writeTenantRuntimeContract(kkaimigrate.DialectPostgres, &output))
	var declaration struct {
		Schema               string `json:"schema"`
		Protocol             string `json:"protocol"`
		PlanChecksum         string `json:"plan_checksum"`
		TenantAware          bool   `json:"tenant_aware"`
		TenantRoutingEnabled bool   `json:"tenant_routing_enabled"`
		SeparateLogDatabase  bool   `json:"separate_log_database"`
		NodeRole             string `json:"node_role"`
		kkaimigrate.SchemaContract
	}
	require.NoError(t, common.Unmarshal(output.Bytes(), &declaration))
	assert.Equal(t, "kkai_tenant_runtime_v1", declaration.Schema)
	assert.Equal(t, "tenant-online-v1", declaration.Protocol)
	assert.Equal(t, kkaimigrate.TenantOnlinePlanChecksum(), declaration.PlanChecksum)
	assert.True(t, declaration.TenantAware)
	assert.False(t, declaration.TenantRoutingEnabled)
	assert.False(t, declaration.SeparateLogDatabase)
	assert.Equal(t, "serving", declaration.NodeRole)
	expected, err := kkaimigrate.ContractForDialect(kkaimigrate.DialectPostgres)
	require.NoError(t, err)
	assert.Equal(t, expected, declaration.SchemaContract)

	t.Setenv("TENANT_ROUTING_ENABLED", "TRUE")
	t.Setenv("LOG_SQL_DSN", "sensitive-never-output")
	output.Reset()
	require.NoError(t, writeTenantRuntimeContract(kkaimigrate.DialectPostgres, &output))
	require.NoError(t, common.Unmarshal(output.Bytes(), &declaration))
	assert.True(t, declaration.TenantRoutingEnabled)
	assert.True(t, declaration.SeparateLogDatabase)
	assert.NotContains(t, output.String(), "sensitive-never-output")
	t.Setenv("TENANT_ROUTING_ENABLED", "1")
	assert.Error(t, writeTenantRuntimeContract(kkaimigrate.DialectPostgres, &output))
}
