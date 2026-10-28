package router

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestShopLiveHTTPServer is an explicit local browser acceptance harness. Run
// TestShopHTTPFixtureSchema first in a fresh disposable PostgreSQL database.
// The application uses its real routes, authentication, storage and workers;
// only the external merchant gateway is simulated. Create STOP in the private
// fixture directory to shut down. It is skipped by every ordinary test run.
func TestShopLiveHTTPServer(t *testing.T) {
	raw, dir := os.Getenv("KKAI_SHOP_HTTP_TEST_DSN"), os.Getenv("KKAI_SHOP_HTTP_TEST_DIR")
	if raw == "" || dir == "" {
		t.Skip("local HTTP harness is not enabled")
	}
	config, err := pgx.ParseConfig(raw)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1", config.Host)
	require.True(t, strings.HasPrefix(config.Database, "kkai_test_shop_http_"))
	require.Empty(t, config.Fallbacks)
	info, err := os.Stat(dir)
	require.NoError(t, err)
	require.True(t, info.IsDir())
	require.Zero(t, info.Mode().Perm()&0077, "credentials directory must be private")
	db, err := gorm.Open(postgres.Open(raw), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis, previousSecureCookie := common.RedisEnabled, common.SessionCookieSecure
	previousPasswordLogin, previousPasswordEncryption := common.PasswordLoginEnabled, common.PasswordLoginEncryptionEnabled
	previousSMTP, previousSetup := common.SMTPServer, constant.Setup
	previousOptions, previousTranslate := common.OptionMap, common.TranslateMessage
	previousHTTPClient, previousGinMode := http.DefaultClient, gin.Mode()
	previousAddress, previousMerchant, previousMerchantKey := operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey
	previousCallback, previousMethods := operation_setting.CustomCallbackAddress, operation_setting.PayMethods
	// Cleanup runs in reverse registration order: workers, HTTP servers, test
	// environment, these globals, then the disposable database connection.
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		common.RedisEnabled, common.SessionCookieSecure = previousRedis, previousSecureCookie
		common.PasswordLoginEnabled, common.PasswordLoginEncryptionEnabled = previousPasswordLogin, previousPasswordEncryption
		common.SMTPServer, constant.Setup = previousSMTP, previousSetup
		common.OptionMap, common.TranslateMessage = previousOptions, previousTranslate
		http.DefaultClient = previousHTTPClient
		gin.SetMode(previousGinMode)
		operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = previousAddress, previousMerchant, previousMerchantKey
		operation_setting.CustomCallbackAddress, operation_setting.PayMethods = previousCallback, previousMethods
	})
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypePostgreSQL, common.DatabaseTypePostgreSQL)
	common.RedisEnabled, common.SessionCookieSecure = false, false
	common.PasswordLoginEnabled, common.PasswordLoginEncryptionEnabled = true, false
	common.SMTPServer = ""
	constant.Setup = true
	common.OptionMap = map[string]string{}
	require.NoError(t, i18n.Init())
	// Init is process-wide and runs once; explicitly bind the helper again if a
	// previous fixture restored a different translator after initialization.
	common.TranslateMessage = i18n.T
	// Historical migration fixtures project only the core columns needed by the
	// migration. Add the remaining application columns without changing any
	// canonical shop table or index.
	require.NoError(t, db.Exec("DELETE FROM users WHERE id = 101").Error)
	for _, value := range []any{&model.User{}, &model.UserSession{}, &model.AuthFlow{}, &model.TwoFA{}, &model.TwoFABackupCode{}, &model.PasskeyCredential{}, &model.UserOAuthBinding{}, &model.TopUp{}, &model.Log{}, &model.KKAIJobLease{}} {
		statement := &gorm.Statement{DB: db}
		require.NoError(t, statement.Parse(value))
		if !db.Migrator().HasTable(value) {
			require.NoError(t, db.AutoMigrate(value))
			continue
		}
		for _, field := range statement.Schema.Fields {
			if field.DBName != "" && !field.IgnoreMigration && !db.Migrator().HasColumn(value, field.DBName) {
				require.NoError(t, db.Migrator().AddColumn(value, field.Name), "%s.%s", statement.Schema.Table, field.DBName)
			}
		}
	}
	// The old logs fixture has no generated IDs; logging must retain its normal
	// INSERT behavior for real login/admin routes.
	require.NoError(t, db.Exec("CREATE SEQUENCE IF NOT EXISTS shop_http_logs_id_seq").Error)
	require.NoError(t, db.Exec("ALTER TABLE logs ALTER COLUMN id SET DEFAULT nextval('shop_http_logs_id_seq')").Error)
	for _, prefix := range []string{"SHOP_SECRET", "SHOP_FINGERPRINT"} {
		t.Setenv(prefix+"_KEYS_FILE", "")
		key := make([]byte, 32)
		_, err = rand.Read(key)
		require.NoError(t, err)
		encoded, err := common.Marshal(map[string]string{"local": base64.StdEncoding.EncodeToString(key)})
		require.NoError(t, err)
		t.Setenv(prefix+"_ACTIVE_KEY_ID", "local")
		t.Setenv(prefix+"_KEYS", string(encoded))
	}
	credentials := map[string]any{"backend_url": "http://localhost:3000", "frontend_url": "http://localhost:5178", "directory": dir}
	for index, name := range []string{"buyer", "admin", "other_buyer"} {
		password := uuid.NewString()
		hash, err := common.HashAccountPassword(password)
		require.NoError(t, err)
		role := common.RoleCommonUser
		if name == "admin" {
			role = common.RoleRootUser
		}
		user := model.User{Id: 1001 + index, Username: "shop_" + name, Password: hash, DisplayName: "Local " + name, Role: role, Status: common.UserStatusEnabled, AuthVersion: 1, Quota: 12345, Group: "default", AffCode: "shop-http-" + name}
		require.NoError(t, db.Create(&user).Error)
		credentials[name] = map[string]any{"username": user.Username, "password": password, "user_id": user.Id}
	}
	merchant, merchantKey := "local-merchant", uuid.NewString()
	var paid sync.Map
	gateway := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		trade := r.URL.Query().Get("out_trade_no")
		var attempt model.ShopPaymentAttempt
		if err := db.Where("trade_no = ?", trade).First(&attempt).Error; err != nil {
			http.Error(w, "unknown local test trade", http.StatusNotFound)
			return
		}
		var order model.ShopOrder
		if err := db.First(&order, attempt.OrderID).Error; err != nil {
			http.Error(w, "unknown local test order", http.StatusNotFound)
			return
		}
		money := fmt.Sprintf("%d.%02d", order.AmountMinor/100, order.AmountMinor%100)
		if r.URL.Path == "/api.php" {
			if r.URL.Query().Get("pid") != merchant || r.URL.Query().Get("key") != merchantKey {
				http.Error(w, "invalid local merchant", http.StatusForbidden)
				return
			}
			status, endtime := 0, ""
			if value, ok := paid.Load(trade); ok {
				status, endtime = 1, value.(time.Time).In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05")
			}
			encoded, _ := common.Marshal(map[string]any{"code": 1, "pid": merchant, "status": status, "trade_no": "sim-" + trade, "out_trade_no": trade, "money": money, "endtime": endtime})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(encoded)
			return
		}
		if r.Method == http.MethodPost {
			paid.LoadOrStore(trade, time.Now())
			params, err := epay.GenerateParamsWithError(map[string]string{"pid": merchant, "trade_no": "sim-" + trade, "out_trade_no": trade, "money": money, "trade_status": epay.StatusTradeSuccess, "type": "alipay"}, merchantKey)
			if err != nil {
				http.Error(w, "local signature failed", http.StatusInternalServerError)
				return
			}
			values := url.Values{}
			for key, value := range params {
				values.Set(key, value)
			}
			response, err := http.PostForm("http://localhost:3000/api/shop/payments/epay/notify", values)
			if err != nil {
				http.Error(w, "local callback failed", http.StatusBadGateway)
				return
			}
			body, _ := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK || string(body) != "success" {
				http.Error(w, "local callback rejected", http.StatusBadGateway)
				return
			}
			http.Redirect(w, r, "http://localhost:5178/shop/orders/"+url.PathEscape(order.PublicID), http.StatusSeeOther)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, "<!doctype html><title>本地模拟支付</title><h1>虚构测试付款</h1><p>仅本机测试，不产生真实资金交易。</p><p>订单 %s · ¥%s</p><form method=post><button type=submit>确认虚构测试付款</button></form>", html.EscapeString(order.PublicID), money)
	}))
	t.Cleanup(gateway.Close)
	http.DefaultClient = gateway.Client()
	operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = gateway.URL, merchant, merchantKey
	operation_setting.CustomCallbackAddress = gateway.URL
	operation_setting.PayMethods = []map[string]string{{"type": "alipay", "name": "本地模拟支付"}}
	credentials["gateway_url"] = gateway.URL
	command := func(version int64) model.ShopCommand {
		key := uuid.NewString()
		digest, keyVersion, err := service.ShopRequestHash(key, "local-http-fixture", "")
		require.NoError(t, err)
		return model.ShopCommand{ActorID: 1002, Admin: true, Key: key, RequestHash: digest, HashKeyVersion: keyVersion, ExpectedVersion: version, Now: time.Now().Unix()}
	}
	retention, err := common.Marshal(model.ShopRetentionPolicy{SecretAfterDeliverySeconds: 86400, BuyerInputAfterClosureSeconds: 86400, EvidenceAfterSettlementSeconds: 86400, AuditAfterSettlementSeconds: 86400, IdempotencyAfterClosureSeconds: 86400})
	require.NoError(t, err)
	reminder, err := common.Marshal(model.ShopReminderPolicy{PaymentUnknownSeconds: 60, DeliveryExceptionSeconds: 60, RefundVerificationSeconds: 60})
	require.NoError(t, err)
	_, err = model.UpdateShopSettings(db, command(0), model.ShopSettings{SalesEnabled: true, Provider: "epay", ProviderAccountRef: "epay:" + merchant, Environment: "test", Currency: "CNY", CurrencyExponent: 2, MinAmountMinor: 1, MaxAmountMinor: 10000, ReservationSeconds: 1800, MaxUnpaidOrders: 10, PolicyVersion: 1, RetentionPolicyJSON: string(retention), ReminderPolicyJSON: string(reminder)}, "Local synthetic acceptance")
	require.NoError(t, err)
	for _, mode := range []string{"automatic", "manual"} {
		terms := model.ShopProductTerms{TemplateVersion: 1, DeliveryFields: []model.ShopField{{Key: "account", Label: "Account", Required: true}, {Key: "password", Label: "Password", Required: true}}, UsageConditions: "Synthetic local test only", DeliveryPolicy: model.ShopDeliveryPolicy{Mode: mode, PublicTerms: "Delivered after confirmed test payment"}, ViewPolicy: model.ShopViewPolicy{DurationSeconds: 3600, FirstStart: "delivered_at", ReplacementStart: "delivered_at", ExpiredAction: "deny_reveal", PublicTerms: "Available for one hour"}, WarrantyPolicy: model.ShopPeriodPolicy{Mode: "duration", StartEvent: "first_delivered_at", DurationSeconds: 86400, PublicTerms: "Synthetic product with one day warranty"}, EntitlementPolicy: model.ShopPeriodPolicy{Mode: "described", PublicTerms: "Synthetic account"}, RefundPolicy: model.ShopRefundPolicy{GoodsRefundMode: "full_only", Execution: "manual_original_channel", PublicTerms: "Full refund after review"}}
		if mode == "manual" {
			terms.DeliveryPolicy.SLA = &model.ShopSLA{Clock: "elapsed", DurationMinutes: 30, Timezone: "Asia/Shanghai"}
		}
		encoded, err := common.Marshal(terms)
		require.NoError(t, err)
		product, err := model.SaveShopProduct(db, command(0), "", model.ShopProduct{Name: "Local " + mode + " account", Specification: "One fictional account for acceptance", ProductKind: "account", DeliveryType: mode, PriceMinor: 125, Currency: "CNY", CurrencyExponent: 2, TermsJSON: string(encoded), InventoryIdentityDomain: "local-http-" + mode, IdentityNormalizationVersion: 1, ManualCapacity: 20})
		require.NoError(t, err)
		product, err = model.SetShopProductStatus(db, command(product.Version), product.PublicID, "published", "")
		require.NoError(t, err)
		credentials[mode+"_product_id"] = product.PublicID
		if mode == "automatic" {
			input := service.ShopInventoryImportRequest{ProductID: product.PublicID, ProductVersion: product.Version, TemplateVersion: 1}
			for index := range 20 {
				identity := fmt.Sprintf("fictional-%02d@example.invalid", index)
				input.Rows = append(input.Rows, service.ShopInventoryRow{Identity: identity, SecretFields: []service.ShopSecretField{{Key: "account", Value: identity}, {Key: "password", Value: "fictional-secret-" + uuid.NewString()}}})
			}
			preview, err := service.PreviewShopInventory(context.Background(), db, command(product.Version), input)
			require.NoError(t, err)
			_, err = service.CommitShopInventory(context.Background(), db, command(product.Version), input, preview.PreviewDigest)
			require.NoError(t, err)
		}
	}
	require.NoError(t, service.CheckShopRuntimeSchema(context.Background(), db))
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(gin.Recovery(), middleware.TenantRouting(db, false, "localhost", "127.0.0.1"))
	SetApiRouter(engine)
	listener, err := net.Listen("tcp", "127.0.0.1:3000")
	require.NoError(t, err)
	server := &http.Server{Handler: engine, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			_ = server.Close()
			t.Errorf("local HTTP server shutdown: %v", err)
		}
	})
	workerID := "shop-http-" + uuid.NewString()
	lease := service.NewKKAILeaderLeaseStore(db, "newapi-background-writer")
	processor := service.NewKKAIOutboxProcessor(db, workerID)
	require.NoError(t, service.RegisterShopOutbox(processor, db, workerID))
	registry := service.NewBackgroundJobRegistry()
	require.NoError(t, service.RegisterShopBackgroundJobs(registry, workerID))
	require.NoError(t, registry.Register(service.BackgroundJob{
		Name: "shop-http-outbox", Interval: 500 * time.Millisecond, RunOnStart: true, WritesData: true, RequiresLeaderLease: true,
		Run: func(ctx context.Context) error { _, err := processor.ProcessBatch(ctx, 20); return err },
	}))
	workerCtx, stopWorker := context.WithCancel(t.Context())
	workerDone := make(chan error, 1)
	go func() {
		workerDone <- registry.Run(workerCtx, service.BackgroundJobRuntime{Role: common.NodeRoleLeader, WriteJobsEnabled: true, HolderID: workerID, Lease: lease, LeaseTTL: time.Minute, LeaseRetryInterval: time.Second})
	}()
	t.Cleanup(func() { stopWorker(); require.NoError(t, <-workerDone) })
	encoded, err := common.Marshal(credentials)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "credentials.json"), encoded, 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "READY"), []byte("http://localhost:3000"), 0600))
	t.Log("local actual HTTP harness ready; credentials are in the private fixture directory")
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-workerCtx.Done():
			return
		case <-ticker.C:
			if _, err := os.Stat(filepath.Join(dir, "STOP")); err == nil {
				return
			}
		}
	}
}
