package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantOnlineInspectionRejectsMutationAndMixedOperations(t *testing.T) {
	for _, operation := range []string{"apply", "finalize", "repair", "unknown"} {
		assert.Error(t, validateTenantOnlineInspection(operation, true, false), operation)
	}
	for _, operation := range []string{"plan", "status", "check"} {
		require.NoError(t, validateTenantOnlineInspection(operation, true, false))
		assert.Error(t, validateTenantOnlineInspection(operation, false, false))
		assert.Error(t, validateTenantOnlineInspection(operation, true, true))
	}
	_, err := applyMigrationTarget(context.Background(), nil, kkaimigrate.TenantOnlineSchemaVersion, kkaimigrate.Options{})
	assert.ErrorIs(t, err, kkaimigrate.ErrUnsafeMigration)
}

func TestTenantOnlineInspectionReadsPristineExpandedAndFinalizedContract(t *testing.T) {
	db, err := openDatabase("file:" + t.TempDir() + "/tenant-online.db")
	require.NoError(t, err)
	prepareLegacyAuthenticationTables(t, db)
	_, err = applyHistoricalFixture(context.Background(), db)
	require.NoError(t, err)
	for _, statement := range []string{
		"ALTER TABLE users ADD COLUMN username VARCHAR(20)",
		"ALTER TABLE users ADD COLUMN email VARCHAR(50)",
		"CREATE UNIQUE INDEX users_username_key ON users(username)",
		"CREATE TABLE top_ups(id BIGINT PRIMARY KEY)",
		"CREATE TABLE user_oauth_bindings(id BIGINT PRIMARY KEY)",
		"CREATE TABLE logs(id BIGINT PRIMARY KEY)",
		"CREATE TABLE quota_data(id BIGINT PRIMARY KEY)",
		"CREATE TABLE tasks(id BIGINT PRIMARY KEY)",
	} {
		require.NoError(t, db.Exec(statement).Error)
	}
	// SQLite's query-only mode rejects every write, including hidden setup DDL.
	require.NoError(t, db.Exec("PRAGMA query_only=ON").Error)
	var output bytes.Buffer
	require.NoError(t, runTenantOnlineInspection(context.Background(), db, "plan", &output))
	var plan kkaimigrate.TenantOnlineExpansionStatus
	require.NoError(t, common.Unmarshal(output.Bytes(), &plan))
	assert.EqualValues(t, 11, plan.CurrentVersion)
	assert.Equal(t, "pristine", plan.State)
	assert.Positive(t, plan.TotalSteps)
	assert.False(t, db.Migrator().HasTable("kkai_tenant_online_steps"))
	assert.False(t, db.Migrator().HasColumn("users", "tenant_id"))
	assert.Error(t, runTenantOnlineInspection(context.Background(), db, "check", &output))
	require.NoError(t, db.Exec("PRAGMA query_only=OFF").Error)
	_, err = kkaimigrate.ApplyTenantOnlineExpansion(context.Background(), db, kkaimigrate.Options{})
	require.NoError(t, err)
	output.Reset()
	require.NoError(t, runTenantOnlineInspection(context.Background(), db, "check", &output))
	require.NoError(t, common.Unmarshal(output.Bytes(), &plan))
	assert.Equal(t, "expanded", plan.State)
	assert.EqualValues(t, 11, plan.CurrentVersion)
	_, err = kkaimigrate.FinalizeTenantOnlineSchema(context.Background(), db, kkaimigrate.Options{})
	require.NoError(t, err)
	output.Reset()
	require.NoError(t, runTenantOnlineInspection(context.Background(), db, "check", &output))
	require.NoError(t, common.Unmarshal(output.Bytes(), &plan))
	assert.Equal(t, "finalized", plan.State)
	assert.EqualValues(t, 14, plan.CurrentVersion)
}
