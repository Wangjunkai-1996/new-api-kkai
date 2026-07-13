package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrateDBCreatesAuthorizationTables(t *testing.T) {
	require.NoError(t, DB.Migrator().DropTable(&CasbinRule{}, &AuthzRole{}))
	t.Cleanup(func() {
		require.NoError(t, DB.AutoMigrate(&CasbinRule{}, &AuthzRole{}))
	})

	require.NoError(t, migrateDB())
	assert.True(t, DB.Migrator().HasTable(&CasbinRule{}))
	assert.True(t, DB.Migrator().HasTable(&AuthzRole{}))
}
