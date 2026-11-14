package kkaimigrate

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestRC49OnlineMigratorDefaultGrantsAllowRuntimeAndReadonlyObservation(t *testing.T) {
	db := newRC49OnlineFixture(t)
	var schema string
	require.NoError(t, db.Raw("SELECT current_schema()").Scan(&schema).Error)
	migrator, application, observer := schema+"_mig", schema+"_app", schema+"_ro"
	const password = "local-rc49-role-test-only"
	for _, role := range []string{migrator, application, observer} {
		require.NoError(t, db.Exec("CREATE ROLE "+role+" LOGIN PASSWORD '"+password+"' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS").Error)
		t.Cleanup(func() {
			require.NoError(t, db.Exec("DROP OWNED BY "+role+" CASCADE").Error)
			require.NoError(t, db.Exec("DROP ROLE "+role).Error)
		})
	}
	// Mirror the installed role boundary: the migrator owns DDL; application
	// receives DML and standby receives SELECT through owner default privileges.
	for _, statement := range []string{
		"GRANT USAGE ON SCHEMA " + schema + " TO " + migrator + "," + application + "," + observer,
		"GRANT CREATE ON SCHEMA " + schema + " TO " + migrator,
		"GRANT SELECT ON ALL TABLES IN SCHEMA " + schema + " TO " + migrator + "," + observer,
		"GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA " + schema + " TO " + application,
		"ALTER ROLE " + observer + " SET default_transaction_read_only=on",
		"ALTER DEFAULT PRIVILEGES FOR ROLE " + migrator + " IN SCHEMA " + schema + " GRANT SELECT,INSERT,UPDATE,DELETE ON TABLES TO " + application,
		"ALTER DEFAULT PRIVILEGES FOR ROLE " + migrator + " IN SCHEMA " + schema + " GRANT SELECT ON TABLES TO " + observer,
		"ALTER TABLE tokens OWNER TO " + migrator,
		"ALTER TABLE quota_data OWNER TO " + migrator,
		"ALTER TABLE audit_logs OWNER TO " + migrator,
		"ALTER TABLE kkai_video_assets OWNER TO " + migrator,
	} {
		require.NoError(t, db.Exec(statement).Error)
	}
	connections := map[string]*gorm.DB{}
	for _, role := range []string{migrator, application, observer} {
		connection, err := gorm.Open(postgres.Open(postgresDSNForRole(t, os.Getenv("KKAI_SHOP_POSTGRES_TEST_DSN"), role, password, schema)), &gorm.Config{})
		require.NoError(t, err)
		sqlDB, err := connection.DB()
		require.NoError(t, err)
		sqlDB.SetMaxOpenConns(1)
		t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
		connections[role] = connection
	}
	ctx := context.Background()
	_, err := ApplyRC49OnlineExpansion(ctx, connections[migrator], Options{Offline: true})
	require.NoError(t, err)
	_, err = ApplyVideoMediaMetadataExpand(ctx, connections[migrator], Options{Offline: true})
	require.NoError(t, err)
	for _, role := range []string{application, observer} {
		connection := connections[role]
		var privileges struct {
			Owner                  string
			CanSelect, CanWrite    bool
			Superuser, CorrectUser bool
		}
		require.NoError(t, connection.Raw(`SELECT pg_get_userbyid(c.relowner) AS owner,
 has_table_privilege(c.oid,'SELECT') AS can_select,
 has_table_privilege(c.oid,'INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER') AS can_write,
 (SELECT rolsuper FROM pg_roles WHERE rolname=current_user) AS superuser,
 current_user=? AS correct_user FROM pg_class c WHERE c.oid=to_regclass('kkai_rc49_online_steps')`, role).Scan(&privileges).Error)
		assert.Equal(t, migrator, privileges.Owner)
		assert.True(t, privileges.CorrectUser)
		assert.False(t, privileges.Superuser)
		assert.True(t, privileges.CanSelect)
		assert.Equal(t, role == application, privileges.CanWrite)
		require.NoError(t, CheckRequired(ctx, connection))
		require.NoError(t, CheckRC49LogSchema(ctx, connection))
		observation, err := Observe(ctx, connection)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{RC49OnlineExpansion: RC49OnlineExpansionDigest(), VideoMediaMetadataExpansion: VideoMediaMetadataExpansionDigest()}, observation.Expansions)
	}
	var readonly string
	require.NoError(t, connections[observer].Raw("SHOW transaction_read_only").Scan(&readonly).Error)
	assert.Equal(t, "on", readonly)
	err = connections[observer].Exec("UPDATE kkai_rc49_online_steps SET plan_checksum='forbidden'").Error
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "25006", pgErr.Code)
}
