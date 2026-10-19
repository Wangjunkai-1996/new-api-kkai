package kkaimigrate

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestTenantOnlinePostgresSelectOnlyRoleObservesExpansionAndContract(t *testing.T) {
	db := newImageAsyncDatabaseFixture(t, DialectPostgres)
	prepareTenantMigrationFixture(t, db)
	ctx := context.Background()
	_, err := ApplyTenantOnlineExpansion(ctx, db, Options{})
	require.NoError(t, err)
	var schemaName string
	require.NoError(t, db.Raw("SELECT current_schema()").Scan(&schemaName).Error)
	roleName := schemaName + "_observer"
	const password = "test-only-tenant-observer"
	require.NoError(t, db.Exec(fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD '%s' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS", roleName, password)).Error)
	t.Cleanup(func() {
		require.NoError(t, db.Exec("REVOKE ALL ON ALL TABLES IN SCHEMA "+schemaName+" FROM "+roleName).Error)
		require.NoError(t, db.Exec("REVOKE ALL ON SCHEMA "+schemaName+" FROM "+roleName).Error)
		require.NoError(t, db.Exec("DROP ROLE "+roleName).Error)
	})
	for _, statement := range []string{
		"ALTER ROLE " + roleName + " SET default_transaction_read_only = on",
		"GRANT USAGE ON SCHEMA " + schemaName + " TO " + roleName,
		"GRANT SELECT ON ALL TABLES IN SCHEMA " + schemaName + " TO " + roleName,
	} {
		require.NoError(t, db.Exec(statement).Error)
	}
	observer, err := gorm.Open(postgres.Open(postgresDSNForRole(t, os.Getenv("KKAI_IMAGE_ASYNC_POSTGRES_TEST_DSN"), roleName, password, schemaName)), &gorm.Config{})
	require.NoError(t, err)
	connection, err := observer.DB()
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	var readonly string
	require.NoError(t, observer.Raw("SHOW transaction_read_only").Scan(&readonly).Error)
	require.Equal(t, "on", readonly)
	var privileges struct{ CorrectRole, Owner, CanSelect, CanWrite bool }
	require.NoError(t, observer.Raw(`SELECT current_user=? AS correct_role,
 pg_catalog.pg_get_userbyid(c.relowner)=current_user AS owner,
 pg_catalog.has_table_privilege(c.oid,'SELECT') AS can_select,
 pg_catalog.has_table_privilege(c.oid,'INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER') AS can_write
FROM pg_catalog.pg_class c WHERE c.oid=pg_catalog.to_regclass('kkai_tenant_online_steps')`, roleName).Scan(&privileges).Error)
	require.True(t, privileges.CorrectRole)
	require.False(t, privileges.Owner)
	require.True(t, privileges.CanSelect)
	require.False(t, privileges.CanWrite)
	plan, err := PlanTenantOnlineExpansion(ctx, observer)
	require.NoError(t, err)
	assert.Equal(t, "expanded", plan.State)
	require.NoError(t, CheckTenantOnlineExpansion(ctx, observer))
	for _, replacement := range []struct{ create, drop string }{
		{"CREATE UNIQUE INDEX receipt_fake_primary ON kkai_tenant_online_steps(step)", "DROP INDEX receipt_fake_primary"},
		{"ALTER TABLE kkai_tenant_online_steps ADD PRIMARY KEY(step,checksum)", "ALTER TABLE kkai_tenant_online_steps DROP CONSTRAINT kkai_tenant_online_steps_pkey"},
	} {
		require.NoError(t, db.Exec("ALTER TABLE kkai_tenant_online_steps DROP CONSTRAINT kkai_tenant_online_steps_pkey").Error)
		require.NoError(t, db.Exec(replacement.create).Error)
		_, err := PlanTenantOnlineExpansion(ctx, observer)
		assert.ErrorIs(t, err, ErrSchemaNotReady, replacement.create)
		assert.ErrorContains(t, err, "kkai_tenant_online_steps has an invalid primary key")
		require.NoError(t, db.Exec(replacement.drop).Error)
		require.NoError(t, db.Exec("ALTER TABLE kkai_tenant_online_steps ADD PRIMARY KEY(step)").Error)
	}
	_, err = FinalizeTenantOnlineSchema(ctx, db, Options{})
	require.NoError(t, err)
	plan, err = PlanTenantOnlineExpansion(ctx, observer)
	require.NoError(t, err)
	assert.Equal(t, "finalized", plan.State)
	assert.EqualValues(t, TenantOnlineSchemaVersion, plan.CurrentVersion)
	require.NoError(t, CheckTenantOnlineSchema(ctx, observer))
	require.NoError(t, CheckRequired(ctx, observer))
}
