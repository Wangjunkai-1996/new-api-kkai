package service

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type membershipRecoveryAcceptanceFixture struct {
	db        *gorm.DB
	pool      *sql.DB
	dialect   string
	namespace string
	users     []model.User
	tokens    []model.Token
	channel   model.Channel
}

// MembershipRecoveryAcceptanceBridge exists only in the test binary. The
// external test package can exercise relay.TextHelper without an import cycle.
type MembershipRecoveryAcceptanceBridge struct {
	DB        *gorm.DB
	Pool      *sql.DB
	Users     []model.User
	Tokens    []model.Token
	Channel   model.Channel
	RunWorker func(context.Context) error
	RunPoll   func(context.Context) error
	SetMode   func(string)
}

// Opt-in real-database acceptance never opens a remote address or touches an
// existing business namespace. These business fixtures use AutoMigrate; the
// production canonical migration contract is validated separately.
func newMembershipRecoveryAcceptanceFixture(t *testing.T, dialect string, members bool, userCount int) *membershipRecoveryAcceptanceFixture {
	t.Helper()
	variable := "KKAI_MEMBERSHIP_RECOVERY_" + strings.ToUpper(dialect) + "_DSN"
	dsn := os.Getenv(variable)
	if dsn == "" {
		t.Skip(variable + " is not configured; real database acceptance was not run")
	}
	require.True(t, model.MembershipEnabled(), "run with KKAI_MEMBERSHIP_ENABLED=true")
	name := "kkai_test_membership_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	var adminDialect, dataDialect gorm.Dialector
	var pgConfig *pgx.ConnConfig
	create, drop := "CREATE SCHEMA "+name, "DROP SCHEMA "+name+" CASCADE"
	dbType := common.DatabaseTypePostgreSQL
	if dialect == "postgres" {
		var err error
		pgConfig, err = pgx.ParseConfig(dsn)
		require.NoError(t, err)
		require.Contains(t, []string{"127.0.0.1", "::1", "localhost"}, pgConfig.Host)
		for _, fallback := range pgConfig.Fallbacks {
			require.Contains(t, []string{"127.0.0.1", "::1", "localhost"}, fallback.Host)
		}
		require.True(t, strings.HasPrefix(pgConfig.Database, "kkai_test_"))
		pgConfig.RuntimeParams["statement_timeout"] = "10000"
		pgConfig.RuntimeParams["lock_timeout"] = "5000"
		pgConfig.RuntimeParams["application_name"] = name
		adminDialect = postgres.New(postgres.Config{Conn: stdlib.OpenDB(*pgConfig)})
	} else {
		config, err := mysqldriver.ParseDSN(dsn)
		require.NoError(t, err)
		require.Equal(t, "tcp", config.Net)
		host, _, err := net.SplitHostPort(config.Addr)
		require.NoError(t, err)
		require.Contains(t, []string{"127.0.0.1", "::1", "localhost"}, host)
		require.True(t, strings.HasPrefix(config.DBName, "kkai_test_"))
		config.Timeout, config.ReadTimeout, config.WriteTimeout = 5*time.Second, 15*time.Second, 15*time.Second
		config.ParseTime = true
		adminDialect = mysql.New(mysql.Config{DSN: config.FormatDSN(), DefaultStringSize: 191})
		config.DBName = name
		dataDialect = mysql.New(mysql.Config{DSN: config.FormatDSN(), DefaultStringSize: 191})
		create, drop = "CREATE DATABASE "+name+" CHARACTER SET utf8mb4", "DROP DATABASE "+name
		dbType = common.DatabaseTypeMySQL
	}
	admin, err := gorm.Open(adminDialect, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	adminSQL, err := admin.DB()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, adminSQL.Close()) })
	require.NoError(t, admin.Exec(create).Error)
	t.Cleanup(func() { assert.NoError(t, admin.Exec(drop).Error) })
	if pgConfig != nil {
		pgConfig.RuntimeParams["search_path"] = name
		dataDialect = postgres.New(postgres.Config{Conn: stdlib.OpenDB(*pgConfig)})
	}
	db, err := gorm.Open(dataDialect, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(8)
	pool.SetMaxIdleConns(8)
	t.Cleanup(func() { assert.NoError(t, pool.Close()) })
	previousDB, previousLog, previousType, previousRedis := model.DB, model.LOG_DB, common.MainDatabaseType(), common.RedisEnabled
	previousGroups := ratio_setting.GroupRatio2JSONString()
	model.DB, model.LOG_DB, common.RedisEnabled = db, db, false
	common.SetMainDatabaseType(dbType)
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"gpt":0.2}`))
	t.Cleanup(func() {
		model.DB, model.LOG_DB, common.RedisEnabled = previousDB, previousLog, previousRedis
		common.SetMainDatabaseType(previousType)
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroups))

	})
	tables := append(model.MembershipModels(), &model.User{}, &model.Token{}, &model.Channel{}, &model.UserSubscription{}, &model.KKAIJobLease{}, &model.Log{})
	tables = append(tables, membershipAcceptanceExtraTables()...)
	require.NoError(t, db.AutoMigrate(tables...))
	configureMembershipRecoveryAcceptanceFixture(t, db)
	if dialect == "mysql" {
		// The fixture's indexed-string default must not cap log payloads. This
		// isolated schema adjustment is not a production migration assertion.
		require.NoError(t, db.Exec("ALTER TABLE logs MODIFY COLUMN other TEXT, MODIFY COLUMN content TEXT").Error)
	}
	catalog, err := model.PublishMembershipCatalog(model.MembershipCatalog{
		ModelLimits:  map[string]model.MembershipModelLimit{"test-model": {MaxInputTokens: 100, MaxOutputTokens: 100, Evidence: "fixed acceptance fixture"}},
		SalesEnabled: true, Tiers: []model.MembershipTier{{ID: "gold", Name: "Gold", Rank: 1, Style: "gold", Currency: "USD", PriceMinor: 1000, Quota: 1_000_000, Enabled: true, Discounts: map[string]float64{"gpt": 0.5}}},
		Groups: []model.MembershipGroup{{Key: "gpt", Enabled: true, Published: true}},
	}, 0, 1)
	require.NoError(t, err)
	fixture := &membershipRecoveryAcceptanceFixture{db: db, pool: pool, dialect: dialect, namespace: name,
		channel: model.Channel{Name: "acceptance", Key: "local-test", Type: 1, Status: 1}}
	require.NoError(t, db.Create(&fixture.channel).Error)
	for i := 0; i < userCount; i++ {
		user := model.User{Username: fmt.Sprintf("member-%d", i), AffCode: fmt.Sprintf("accept-%d", i), Group: "default", Quota: 1_000_000, Status: common.UserStatusEnabled}
		require.NoError(t, db.Create(&user).Error)
		token := model.Token{UserId: user.Id, Key: fmt.Sprintf("acceptance-token-%d", i), Status: common.TokenStatusEnabled, RemainQuota: 1_000_000, ExpiredTime: -1}
		require.NoError(t, db.Create(&token).Error)
		fixture.users, fixture.tokens = append(fixture.users, user), append(fixture.tokens, token)
		if members {
			order, err := model.CreateMembershipOrder(model.MembershipOrderInput{UserID: user.Id, TierID: "gold", ReleaseID: catalog.ReleaseID, Provider: model.PaymentProviderEpay, PaymentMethod: "alipay", TradeNo: fmt.Sprintf("acceptance-order-%d", i), ExpiresAt: common.GetTimestamp() + 600})
			require.NoError(t, err)
			_, err = model.CompleteMembershipPayment(model.MembershipPaymentInput{TradeNo: order.TradeNo, Provider: order.Provider, ReceiptID: fmt.Sprintf("acceptance-receipt-%d", i), AmountMinor: order.AmountMinor, Currency: order.Currency, OneTime: true, Paid: true})
			require.NoError(t, err)
		}
	}
	require.NoError(t, db.Create(&model.KKAIJobLease{LeaseName: "newapi-background-writer", Holder: "acceptance-worker", Fence: 1, LeaseUntil: time.Now().Unix() + 3600}).Error)
	return fixture
}

func (f *membershipRecoveryAcceptanceFixture) reservationInput(t *testing.T, id string, userIndex int, maximum float64) model.MembershipReservationInput {
	t.Helper()
	user, token := f.users[userIndex], f.tokens[userIndex]
	info := &relaycommon.RelayInfo{UserId: user.Id, TokenId: token.Id, TokenKey: token.Key, RequestId: id, UsingGroup: "gpt", OriginModelName: "test-model", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: f.channel.Id}, PriceData: types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.2}}}
	input, err := PrepareMembershipReservation(info, maximum)
	require.NoError(t, err)
	return input
}

// Both exact source revisions receive the same conflicting consume logs and
// aged reservations. Only the new runtime also has its persistent conflict queue.
func seedMembershipAcceptanceBacklog(t *testing.T, f *membershipRecoveryAcceptanceFixture, conflicts int, unlimited bool) []model.MembershipBillingRecord {
	t.Helper()
	for i := range f.tokens {
		f.tokens[i].UnlimitedQuota = unlimited
		require.NoError(t, f.db.Model(&f.tokens[i]).Update("unlimited_quota", unlimited).Error)
	}
	aged := time.Now().Unix() - 73*3600
	records := make([]model.MembershipBillingRecord, 0, conflicts)
	logs := make([]model.Log, 0, conflicts)
	for i := 0; i < conflicts; i++ {
		id := fmt.Sprintf("conflict-%04d", i)
		record, err := model.ReserveMembershipBilling(f.reservationInput(t, id, i%len(f.users), 100))
		require.NoError(t, err)
		require.NoError(t, model.MarkMembershipBillingDispatched(id, f.channel.Id, time.Now().Unix()+300))
		require.NoError(t, f.db.Model(record).Updates(map[string]any{"created_at": aged, "review_started_at": aged + 1, "review_after": aged + 1, "review_reason": "acceptance_conflict"}).Error)
		record.CreatedAt = aged
		records = append(records, *record)
		logs = append(logs, model.Log{RequestId: id, UserId: record.UserID, TokenId: record.TokenID, ChannelId: f.channel.Id,
			Type: model.LogTypeConsume, Quota: 3, Other: `{"billing_source":"conflicting_fixture"}`})
	}
	if len(logs) > 0 {
		require.NoError(t, f.db.CreateInBatches(logs, 100).Error)
	}
	return records
}
