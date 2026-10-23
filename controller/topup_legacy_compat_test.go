package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func walletLegacyDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "wallet-legacy.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	previous := model.DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	model.DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB = previous
		common.SetDatabaseTypes(previousMain, previousLog)
		require.NoError(t, sqlDB.Close())
	})
	for _, ddl := range []string{
		"CREATE TABLE users(id INTEGER PRIMARY KEY, telegram_id TEXT, username VARCHAR(20), email VARCHAR(50))",
		"CREATE UNIQUE INDEX uni_users_username ON users(username)",
		"CREATE TABLE tokens(id INTEGER PRIMARY KEY)",
		"CREATE TABLE passkey_credentials(id INTEGER PRIMARY KEY)",
		"CREATE TABLE options(key VARCHAR(255) PRIMARY KEY, value TEXT)",
		"CREATE TABLE midjourneys(id INTEGER PRIMARY KEY, mj_id TEXT, channel_id BIGINT, quota BIGINT, status TEXT)",
		"CREATE TABLE top_ups(id INTEGER PRIMARY KEY, user_id INTEGER, trade_no VARCHAR(255), status VARCHAR(32), create_time INTEGER)",
		"CREATE TABLE user_oauth_bindings(id INTEGER PRIMARY KEY)",
		"CREATE TABLE logs(id INTEGER PRIMARY KEY)",
		"CREATE TABLE quota_data(id INTEGER PRIMARY KEY)",
		"CREATE TABLE tasks(id INTEGER PRIMARY KEY)",
	} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	for _, apply := range []func(context.Context, *gorm.DB, kkaimigrate.Options) (*kkaimigrate.Result, error){
		kkaimigrate.ApplyLegacyBaseline, kkaimigrate.ApplyOutboxEventKeyCompatibility,
		kkaimigrate.ApplyVideoStudioExpand, kkaimigrate.ApplyVideoSampleCategoryExpand,
		kkaimigrate.ApplyImageStudioExpand, kkaimigrate.ApplyAuthenticationExpand,
		kkaimigrate.ApplyRC41ArchitectureExpand, kkaimigrate.ApplyImageAsyncExpand,
		kkaimigrate.ApplyMembershipExpand, kkaimigrate.ApplyTenantOnlineExpansion,
		kkaimigrate.FinalizeTenantOnlineSchema, kkaimigrate.ApplyResellerApplicationExpand,
		kkaimigrate.ApplyRequestTicketExpand, kkaimigrate.ApplyInvoiceExpand,
	} {
		_, err := apply(context.Background(), db, kkaimigrate.Options{Offline: true})
		require.NoError(t, err)
	}
	for _, value := range []any{&model.User{}, &model.TopUp{}} {
		statement := &gorm.Statement{DB: db}
		require.NoError(t, statement.Parse(value))
		columns, err := db.Migrator().ColumnTypes(value)
		require.NoError(t, err)
		present := make(map[string]bool, len(columns))
		for _, column := range columns {
			present[column.Name()] = true
		}
		for _, field := range statement.Schema.Fields {
			if statement.Schema.Table == "top_ups" && field.Tag.Get("json") == "-" {
				continue
			}
			if field.DBName != "" && !field.IgnoreMigration && !present[field.DBName] {
				require.NoError(t, db.Migrator().AddColumn(value, field.Name))
			}
		}
	}
	return db
}

func enableWalletLegacyPayments(t *testing.T) {
	t.Helper()
	confirmPaymentComplianceForTest(t)
	for target, value := range map[*string]string{
		&operation_setting.PayAddress: "https://merchant.example.test", &operation_setting.EpayId: "123", &operation_setting.EpayKey: "legacy-test-secret",
		&setting.StripeApiSecret: "sk_test_legacy", &setting.StripeWebhookSecret: "whsec_legacy", &setting.StripePriceId: "price_legacy",
		&setting.CreemApiKey: "creem_test", &setting.CreemProducts: `[{"productId":"prod_test"}]`,
		&setting.WaffoApiKey: "waffo_test", &setting.WaffoPrivateKey: "waffo_test", &setting.WaffoPublicCert: "waffo_test",
		&setting.WaffoPancakeMerchantID: "pancake_test", &setting.WaffoPancakePrivateKey: "pancake_test", &setting.WaffoPancakeProductID: "pancake_test",
	} {
		original := *target
		*target = value
		t.Cleanup(func() { *target = original })
	}
	oldMethods := operation_setting.PayMethods
	oldWaffoEnabled, oldWaffoSandbox := setting.WaffoEnabled, setting.WaffoSandbox
	t.Cleanup(func() {
		operation_setting.PayMethods = oldMethods
		setting.WaffoEnabled, setting.WaffoSandbox = oldWaffoEnabled, oldWaffoSandbox
	})
	operation_setting.PayMethods = []map[string]string{{"name": "Alipay", "type": "alipay"}}
	setting.WaffoEnabled, setting.WaffoSandbox = true, false
}

func invokeWalletCompatibility(t *testing.T, handler gin.HandlerFunc, body string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/test-wallet", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("id", 7)
	handler(ctx)
	return response
}

func TestWalletLegacyFiveRoutesRemainAvailableOnCanonical17(t *testing.T) {
	walletLegacyDB(t)
	enableWalletLegacyPayments(t)
	for name, handler := range map[string]gin.HandlerFunc{
		"epay": RequestEpay, "stripe": RequestStripePay, "creem": RequestCreemPay,
		"waffo": RequestWaffoPay, "waffo_pancake": RequestWaffoPancakePay,
	} {
		t.Run(name, func(t *testing.T) {
			response := invokeWalletCompatibility(t, handler, "{")
			assert.Equal(t, http.StatusOK, response.Code)
			assert.Contains(t, response.Body.String(), "参数错误")
		})
	}
	response := invokeWalletCompatibility(t, GetTopUpInfo, "")
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), `"wallet_lifecycle_ready":false`)
}

func TestWalletLegacyEpayCreatesExactCashSnapshotWithoutV18Objects(t *testing.T) {
	db := walletLegacyDB(t)
	enableWalletLegacyPayments(t)
	oldLogDB, oldSchemaMode := model.LOG_DB, common.SchemaManagementMode
	t.Setenv("LOG_SQL_DSN", "")
	common.SchemaManagementMode = common.SchemaManagementExternal
	require.NoError(t, model.InitLogDB()) // Initialize the same dialect column quoting as application startup.
	common.SchemaManagementMode = oldSchemaMode
	t.Cleanup(func() { model.LOG_DB = oldLogDB })
	oldMin, oldPrice, oldQuota := operation_setting.MinTopUp, operation_setting.Price, common.QuotaPerUnit
	oldDisplay := operation_setting.GetGeneralSetting().QuotaDisplayType
	t.Cleanup(func() {
		operation_setting.MinTopUp, operation_setting.Price, common.QuotaPerUnit = oldMin, oldPrice, oldQuota
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplay
	})
	operation_setting.MinTopUp, operation_setting.Price, common.QuotaPerUnit = 1, 2.5, 500000
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	require.NoError(t, db.Create(&model.User{Id: 7, Username: "legacy-buyer", Group: "default", AffCode: "legacy-buyer"}).Error)
	response := invokeWalletCompatibility(t, RequestEpay, `{"amount":10,"payment_method":"alipay"}`)
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), `"message":"success"`)
	var orders []model.TopUp
	require.NoError(t, db.Find(&orders).Error)
	require.Len(t, orders, 1)
	assert.EqualValues(t, 2500, orders[0].PaidMinor)
	assert.Equal(t, "CNY", orders[0].Currency)
	assert.Equal(t, "pending", orders[0].Status)
	assert.Zero(t, orders[0].WalletLifecycleVersion)
	present, err := kkaimigrate.WalletPaymentSchemaObjectsPresent(db)
	require.NoError(t, err)
	assert.False(t, present)

	_, err = kkaimigrate.ApplyWalletPaymentExpand(context.Background(), db, kkaimigrate.Options{Offline: true})
	require.NoError(t, err)
	late := &model.TopUp{UserId: 7, TradeNo: "late-legacy-order"}
	assert.ErrorIs(t, late.InsertBeforeWalletPaymentExpand(context.Background(), checkLegacyWalletSchema), model.ErrWalletPaymentSchema)
	var count int64
	require.NoError(t, db.Model(&model.TopUp{}).Count(&count).Error)
	assert.EqualValues(t, 1, count, "the final schema check must reject a stale pre-migration purchase")
}

func TestWalletCompatibilityRejectsUnknownAndPartialSchemas(t *testing.T) {
	for _, damage := range []struct {
		name string
		ddl  string
	}{
		{"unknown_receipt", "UPDATE kkai_schema_migrations SET checksum = 'unknown' WHERE version = 17"},
		{"partial_column", "ALTER TABLE top_ups ADD COLUMN wallet_lifecycle_version INTEGER NULL"},
		{"partial_table", "CREATE TABLE wallet_topup_flows (id INTEGER PRIMARY KEY)"},
	} {
		t.Run(damage.name, func(t *testing.T) {
			db := walletLegacyDB(t)
			require.NoError(t, db.Exec(damage.ddl).Error)
			for _, handler := range []gin.HandlerFunc{RequestEpay, GetTopUpInfo} {
				response := invokeWalletCompatibility(t, handler, `{"amount":10,"payment_method":"alipay"}`)
				assert.Equal(t, http.StatusServiceUnavailable, response.Code)
				assert.Contains(t, response.Body.String(), "WALLET_STATE_UNAVAILABLE")
			}
		})
	}
}

func TestWalletCompatibilityCanonical18RequiresUpdatedClient(t *testing.T) {
	db := walletCallbackDB(t)
	for name, handler := range map[string]gin.HandlerFunc{
		"epay": RequestEpay, "stripe": RequestStripePay, "creem": RequestCreemPay,
		"waffo": RequestWaffoPay, "waffo_pancake": RequestWaffoPancakePay,
	} {
		t.Run(name, func(t *testing.T) {
			response := invokeWalletCompatibility(t, handler, `{"amount":10,"payment_method":"stripe","product_id":"prod_test"}`)
			assert.Equal(t, http.StatusConflict, response.Code)
			assert.Contains(t, response.Body.String(), "CLIENT_UPGRADE_REQUIRED")
		})
	}
	response := invokeWalletCompatibility(t, GetTopUpInfo, "")
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), `"wallet_lifecycle_ready":true`)
	var count int64
	require.NoError(t, db.Model(&model.TopUp{}).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, db.Exec("DROP INDEX idx_wallet_flow_owner").Error)
	// Runtime readiness intentionally shares a bounded successful audit. A new
	// DB instance forces revalidation here; maintenance checks remain uncached.
	model.DB = db.Session(&gorm.Session{})
	response = invokeWalletCompatibility(t, GetTopUpInfo, "")
	assert.Equal(t, http.StatusServiceUnavailable, response.Code, "schema damage must not be mistaken for a legacy wallet")
}

func TestWalletCompatibilityUnavailableDatabaseCannotSelectLegacy(t *testing.T) {
	previous := model.DB
	model.DB = nil
	t.Cleanup(func() { model.DB = previous })
	for _, handler := range []gin.HandlerFunc{RequestEpay, GetTopUpInfo} {
		response := invokeWalletCompatibility(t, handler, `{"amount":10,"payment_method":"alipay"}`)
		assert.Equal(t, http.StatusServiceUnavailable, response.Code)
		assert.Contains(t, response.Body.String(), "WALLET_STATE_UNAVAILABLE")
	}
}
