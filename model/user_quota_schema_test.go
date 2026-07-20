package model

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestUserQuotaSchemaUsesBigIntAcrossDialects(t *testing.T) {
	tests := []struct {
		name      string
		dialector gorm.Dialector
	}{
		{
			name: "sqlite",
			dialector: sqlite.Open(
				"file:user-quota-schema?mode=memory&cache=shared",
			),
		},
		{
			name: "mysql",
			dialector: mysql.New(mysql.Config{
				DSN:                       "newapi:test@tcp(127.0.0.1:3306)/newapi?parseTime=true",
				SkipInitializeWithVersion: true,
			}),
		},
		{
			name: "postgres",
			dialector: postgres.New(postgres.Config{
				DSN: "postgres://newapi:test@127.0.0.1:5432/newapi?sslmode=disable",
			}),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, err := gorm.Open(test.dialector, &gorm.Config{
				DisableAutomaticPing: true,
				DryRun:               true,
			})
			require.NoError(t, err)

			statement := &gorm.Statement{DB: db}
			require.NoError(t, statement.Parse(&User{}))
			for _, fieldName := range []string{"Quota", "UsedQuota"} {
				field := statement.Schema.LookUpField(fieldName)
				require.NotNil(t, field)
				dataType := strings.ToLower(db.Migrator().FullDataTypeOf(field).SQL)
				require.Contains(t, dataType, "bigint", fieldName)
			}
		})
	}
}

func TestUserQuotaSQLiteAutoMigrateStoresInt64Values(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "user-quota-int64.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&User{}))

	expected := int64(5_000_000_000)
	require.NoError(t, db.Create(&User{
		Id:        1,
		Username:  "bigint-wallet",
		Quota:     expected,
		UsedQuota: expected + 1,
	}).Error)

	var stored User
	require.NoError(t, db.First(&stored, 1).Error)
	require.Equal(t, expected, stored.Quota)
	require.Equal(t, expected+1, stored.UsedQuota)
}
