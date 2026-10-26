package model

import (
	"fmt"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/clickhouse"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestLogBillingSourceFilterDatabaseMatrix(t *testing.T) {
	for _, dialect := range []common.DatabaseType{common.DatabaseTypeSQLite, common.DatabaseTypePostgreSQL, common.DatabaseTypeMySQL, common.DatabaseTypeClickHouse} {
		t.Run(string(dialect), func(t *testing.T) {
			var driver gorm.Dialector
			if dialect == common.DatabaseTypeSQLite {
				driver = sqlite.Open(":memory:")
			} else {
				env := map[common.DatabaseType]string{common.DatabaseTypePostgreSQL: "TEST_POSTGRES_DSN", common.DatabaseTypeMySQL: "TEST_MYSQL_DSN", common.DatabaseTypeClickHouse: "TEST_CLICKHOUSE_DSN"}[dialect]
				dsn := os.Getenv(env)
				if dsn == "" {
					t.Skip(env + " is not configured")
				}
				switch dialect {
				case common.DatabaseTypePostgreSQL:
					driver = postgres.Open(dsn)
				case common.DatabaseTypeMySQL:
					driver = mysql.Open(dsn)
				case common.DatabaseTypeClickHouse:
					driver = clickhouse.Open(dsn)
				}
			}
			db, err := gorm.Open(driver, &gorm.Config{})
			require.NoError(t, err)
			previousType := common.LogDatabaseType()
			common.SetLogDatabaseType(dialect)
			t.Cleanup(func() {
				common.SetLogDatabaseType(previousType)
				sqlDB, err := db.DB()
				require.NoError(t, err)
				require.NoError(t, sqlDB.Close())
			})
			for _, test := range []struct {
				name   string
				other  string
				typeID int
				source string
			}{
				{"member", `{"billing_source":"membership"}`, LogTypeConsume, MembershipSource},
				{"member spaced JSON", `{ "billing_source" : "membership" }`, LogTypeConsume, MembershipSource},
				{"wallet fallback", `{"billing_source":"wallet","membership_account_id":1}`, LogTypeConsume, MembershipWalletSource},
				{"image wallet", `{"billing_source":"member_wallet"}`, LogTypeConsume, MembershipWalletSource},
				{"legacy empty", "", LogTypeConsume, MembershipWalletSource},
				{"legacy JSON", `{"model_ratio":1}`, LogTypeConsume, MembershipWalletSource},
				{"nested source", `{"admin_info":{"billing_source":"membership"}}`, LogTypeConsume, MembershipWalletSource},
				{"source in text", `{"note":"\"billing_source\":\"membership\""}`, LogTypeConsume, MembershipWalletSource},
				{"legacy subscription", `{"billing_source":"subscription"}`, LogTypeConsume, ""},
				{"unknown source", `{"billing_source":"future-source"}`, LogTypeConsume, ""},
				{"refund", `{"billing_source":"membership"}`, LogTypeRefund, ""},
				{"nonbilling", "", LogTypeManage, ""},
			} {
				t.Run(test.name, func(t *testing.T) {
					for _, source := range []string{MembershipSource, MembershipWalletSource} {
						tx := db.Table(fmt.Sprintf("(SELECT ? AS other, %d AS type) AS logs", test.typeID), test.other)
						tx, err := applyLogBillingSourceFilter(tx, source)
						require.NoError(t, err)
						var count int64
						require.NoError(t, tx.Count(&count).Error)
						assert.Equal(t, test.source == source, count == 1, source)
					}
				})
			}
		})
	}
}
