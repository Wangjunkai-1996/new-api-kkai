//go:build !kkai_bridge

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

func TestResellerPrefixCommandNeverExposesUnguardedMutation(t *testing.T) {
	for _, operation := range []string{"plan", "status"} {
		require.NoError(t, validateResellerPrefixCommand(operation, "", true, false))
		assert.Error(t, validateResellerPrefixCommand(operation, tenantControllerProofPath, true, false))
	}
	assert.Error(t, validateResellerPrefixCommand("apply", tenantControllerProofPath, true, false))
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
