package model

import (
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Opt-in acceptance uses two independent sessions and a fresh namespace. It
// rejects nonlocal DSNs and never migrates or drops existing application tables.
func shopLiveDatabase(t *testing.T, dialect string) (*gorm.DB, *gorm.DB) {
	t.Helper()
	variable := "KKAI_SHOP_" + strings.ToUpper(dialect) + "_TEST_DSN"
	raw := os.Getenv(variable)
	if raw == "" {
		t.Skip(variable + " is not set; live database acceptance was not run")
	}
	name := "kkai_shop_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	var adminDialect, dataDialect gorm.Dialector
	var pgConfig *pgx.ConnConfig
	create, drop := "CREATE SCHEMA "+name, "DROP SCHEMA "+name+" CASCADE"
	if dialect == "postgres" {
		var err error
		pgConfig, err = pgx.ParseConfig(raw)
		require.NoError(t, err)
		require.Contains(t, []string{"127.0.0.1", "::1", "localhost"}, pgConfig.Host)
		for _, fallback := range pgConfig.Fallbacks {
			require.Contains(t, []string{"127.0.0.1", "::1", "localhost"}, fallback.Host)
		}
		require.True(t, strings.HasPrefix(pgConfig.Database, "kkai_test_"), "requires a dedicated kkai_test_ database")
		pgConfig.RuntimeParams["statement_timeout"] = "10000"
		pgConfig.RuntimeParams["lock_timeout"] = "5000"
		adminDialect = postgres.New(postgres.Config{Conn: stdlib.OpenDB(*pgConfig)})
	} else {
		config, err := mysqldriver.ParseDSN(raw)
		require.NoError(t, err)
		require.Equal(t, "tcp", config.Net)
		host, _, err := net.SplitHostPort(config.Addr)
		require.NoError(t, err)
		require.Contains(t, []string{"127.0.0.1", "::1", "localhost"}, host)
		require.True(t, strings.HasPrefix(config.DBName, "kkai_test_"), "requires a dedicated kkai_test_ database")
		config.Timeout, config.ReadTimeout, config.WriteTimeout = 5*time.Second, 15*time.Second, 15*time.Second
		adminDialect = mysql.Open(config.FormatDSN())
		config.DBName = name
		dataDialect = mysql.Open(config.FormatDSN())
		create, drop = "CREATE DATABASE "+name+" CHARACTER SET utf8mb4", "DROP DATABASE "+name
	}
	admin, err := gorm.Open(adminDialect, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	adminSQL, err := admin.DB()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, adminSQL.Close()) })
	require.NoError(t, admin.Exec(create).Error)
	t.Log("isolated namespace:", name)
	t.Cleanup(func() { assert.NoError(t, admin.Exec(drop).Error) })
	connections := make([]*gorm.DB, 0, 2)
	for range 2 {
		if pgConfig != nil {
			pgConfig.RuntimeParams["search_path"] = name
			dataDialect = postgres.New(postgres.Config{Conn: stdlib.OpenDB(*pgConfig)})
		}
		db, err := gorm.Open(dataDialect, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		require.NoError(t, err)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		sqlDB.SetMaxOpenConns(1)
		sqlDB.SetMaxIdleConns(1)
		t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
		connections = append(connections, db)
	}
	query := "SELECT pg_backend_pid()"
	dbType := common.DatabaseTypePostgreSQL
	if dialect == "mysql" {
		query, dbType = "SELECT CONNECTION_ID()", common.DatabaseTypeMySQL
	}
	var firstID, secondID int64
	require.NoError(t, connections[0].Raw(query).Scan(&firstID).Error)
	require.NoError(t, connections[1].Raw(query).Scan(&secondID).Error)
	require.NotEqual(t, firstID, secondID, "contention requires distinct database sessions")
	previous := common.MainDatabaseType()
	common.SetMainDatabaseType(dbType)
	t.Cleanup(func() { common.SetMainDatabaseType(previous) })
	db := connections[0]
	require.NoError(t, db.AutoMigrate(append(ShopSchemaModels(), &KKAIOutboxEvent{}, &KKAIJobLease{})...))
	// Shop reads only this existing user contract; full user-schema migrations
	// are outside this business-flow fixture and are verified separately.
	require.NoError(t, db.Table("users").AutoMigrate(&struct {
		ID        int `gorm:"primaryKey"`
		TenantID  int64
		Role      int
		Status    int
		DeletedAt gorm.DeletedAt
	}{}))
	require.NoError(t, db.Table("users").Create([]map[string]any{{"id": 1, "tenant_id": PlatformTenantID, "role": common.RoleCommonUser, "status": common.UserStatusEnabled}, {"id": 2, "tenant_id": PlatformTenantID, "role": common.RoleAdminUser, "status": common.UserStatusEnabled}, {"id": 3, "tenant_id": PlatformTenantID, "role": common.RoleCommonUser, "status": common.UserStatusEnabled}}).Error)
	return db, connections[1]
}

func TestShopLiveDatabaseContracts(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			t.Run("order_idempotency_and_last_stock", func(t *testing.T) {
				db, second := shopLiveDatabase(t, dialect)
				terms := ShopProductTerms{TemplateVersion: 1, DeliveryFields: []ShopField{{Key: "account", Label: "Account"}}, UsageConditions: "Personal use", DeliveryPolicy: ShopDeliveryPolicy{Mode: "automatic", PublicTerms: "After payment"}, ViewPolicy: ShopViewPolicy{DurationSeconds: 600, FirstStart: "delivered_at", ReplacementStart: "delivered_at", ExpiredAction: "deny_reveal", PublicTerms: "Ten minutes"}, WarrantyPolicy: ShopPeriodPolicy{Mode: "none", PublicTerms: "No continuing warranty"}, EntitlementPolicy: ShopPeriodPolicy{Mode: "described", PublicTerms: "One account"}, RefundPolicy: ShopRefundPolicy{GoodsRefundMode: "full_only", Execution: "manual_original_channel", PublicTerms: "Review required"}}
				encoded, err := common.Marshal(terms)
				require.NoError(t, err)
				require.NoError(t, db.Create(&ShopSettings{ID: 1, Version: 1, SalesEnabled: true, Currency: "CNY", CurrencyExponent: 2, MinAmountMinor: 1, MaxAmountMinor: 10000, ReservationSeconds: 300, MaxUnpaidOrders: 2, RetentionPolicyJSON: "{}"}).Error)
				product, err := SaveShopProduct(db, shopTestCommand("live-product-create", 0, 100), "", ShopProduct{Name: "Account", DeliveryType: "automatic", PriceMinor: 125, Currency: "CNY", CurrencyExponent: 2, TermsJSON: string(encoded), InventoryIdentityDomain: "account-provider", IdentityNormalizationVersion: 1})
				require.NoError(t, err)
				product, err = SetShopProductStatus(db, shopTestCommand("live-product-publish", product.Version, 101), product.PublicID, "published", "")
				require.NoError(t, err)
				_, err = ImportShopInventory(db, ShopImportInput{Command: shopTestCommand("live-inventory-import", product.Version, 102), ProductPublicID: product.PublicID, ProductVersion: product.Version, TemplateVersion: 1, Rows: []ShopStockImportRow{{PublicID: "last-unit", Secret: ShopSecret{Ciphertext: "encrypted-account", KeyVersion: "v1"}, DomainHash: strings.Repeat("a", 64), Fingerprint: strings.Repeat("b", 64), FingerprintKeyVersion: "fp1", NormalizationVersion: 1}}})
				require.NoError(t, err)
				command := shopTestCommand("live-same-order-key", 0, 200)
				command.ActorID, command.Admin = 1, false
				input := ShopCreateOrderInput{Command: command, ProductPublicID: product.PublicID, ProductVersion: product.Version, TermsVersion: product.TermsVersion, AcceptedTerms: true}
				type outcome struct {
					order *ShopOrder
					err   error
				}
				start, results := make(chan struct{}), make(chan outcome, 2)
				snapshotReady := []chan struct{}{make(chan struct{}), make(chan struct{})}
				resume := make(chan struct{})
				for i, connection := range []*gorm.DB{db, second} {
					var once sync.Once
					// Both authorizations complete before either caller takes the
					// buyer lock, making the old repeatable-read failure deterministic.
					require.NoError(t, connection.Callback().Query().After("gorm:query").Register("shop:test_authorization_snapshot", func(tx *gorm.DB) {
						if tx.Statement.Table == "users" {
							once.Do(func() { close(snapshotReady[i]); <-resume })
						}
					}))
					t.Cleanup(func() { assert.NoError(t, connection.Callback().Query().Remove("shop:test_authorization_snapshot")) })
					go func() {
						<-start
						order, err := CreateShopOrder(connection, input)
						results <- outcome{order, err}
					}()
				}
				close(start)
				for _, ready := range snapshotReady {
					<-ready
				}
				close(resume)
				first, replay := <-results, <-results
				require.NoError(t, first.err)
				require.NoError(t, replay.err)
				assert.Equal(t, first.order.ID, replay.order.ID)
				input.Command.ActorID, input.Command.Key = 3, "live-second-order"
				_, err = CreateShopOrder(second, input)
				assert.ErrorIs(t, err, ErrShopStock)
				var count int64
				require.NoError(t, db.Model(&ShopOrder{}).Count(&count).Error)
				assert.Equal(t, int64(1), count)
				var stock ShopStockUnit
				require.NoError(t, db.First(&stock).Error)
				assert.Equal(t, "reserved", stock.State)
				require.NotNil(t, stock.ReservedOrderID)
				assert.Equal(t, first.order.ID, *stock.ReservedOrderID)
			})
			t.Run("concurrent_inventory_candidates", func(t *testing.T) {
				db, second := shopLiveDatabase(t, dialect)
				first, receipt, stock := shopTestPaidInventory(t, db)
				other := first
				other.ShopRecord = ShopRecord{PublicID: "second-order", Version: 1}
				other.UserID, other.ActiveAttemptID, other.EffectiveReceiptID = 3, nil, nil
				require.NoError(t, db.Create(&other).Error)
				otherReceipt := receipt
				otherReceipt.ShopRecord = ShopRecord{PublicID: "second-receipt", Version: 1}
				otherReceipt.ReceiptKey, otherReceipt.ProviderTransactionID = "second-receipt", "second-transaction"
				otherReceipt.OrderID, otherReceipt.AttemptID = &other.ID, nil
				require.NoError(t, db.Create(&otherReceipt).Error)
				require.NoError(t, db.Model(&other).Update("effective_receipt_id", otherReceipt.ID).Error)
				type outcome struct {
					index int
					err   error
				}
				start, results := make(chan struct{}), make(chan outcome, 2)
				orders, connections := []ShopOrder{first, other}, []*gorm.DB{db, second}
				for i, order := range orders {
					candidate, err := GetShopFulfillmentCandidate(connections[i], order.PublicID)
					require.NoError(t, err)
					require.Equal(t, stock.ID, candidate.ID)
					go func() {
						<-start
						_, err := FulfillShopOrder(connections[i], ShopFulfillInput{Command: ShopCommand{Now: 210}, OrderPublicID: order.PublicID, DeliveryPublicID: fmt.Sprintf("delivery-%d", i), StockUnitID: candidate.ID, StockVersion: candidate.Version})
						results <- outcome{i, err}
					}()
				}
				close(start)
				successes, winner := 0, -1
				for range orders {
					result := <-results
					if result.err == nil {
						successes++
						winner = result.index
					} else {
						assert.ErrorIs(t, result.err, ErrShopVersion)
					}
				}
				require.Equal(t, 1, successes)
				require.NoError(t, db.First(&stock, stock.ID).Error)
				require.NotNil(t, stock.SoldOrderID)
				assert.Equal(t, orders[winner].ID, *stock.SoldOrderID)
				var count int64
				require.NoError(t, db.Model(&ShopDelivery{}).Count(&count).Error)
				assert.Equal(t, int64(1), count)
			})
			t.Run("refund_delivery_reveal_fence", func(t *testing.T) {
				db, second := shopLiveDatabase(t, dialect)
				order, receipt, stock := shopTestPaidInventory(t, db)
				evidence, err := RegisterShopPaymentEvidence(db, ShopEvidenceInput{Command: shopTestCommand("live-funds-evidence", receipt.Version, 201), ReceiptPublicID: receipt.PublicID, Evidence: ShopPaymentEvidence{Source: "merchant_console", ProviderTransactionID: receipt.ProviderTransactionID, AmountMinor: 100, Currency: "CNY", CurrencyExponent: 2, Reference: "merchant/transaction", Summary: "Not refunded", ObservedStatus: "not_refunded", ObservedAt: 201}})
				require.NoError(t, err)
				require.NoError(t, db.First(&receipt, receipt.ID).Error)
				refund, err := ApproveShopRefund(db, ShopRefundApproveInput{Command: shopTestCommand("live-refund-approve", receipt.Version, 202), ReceiptPublicID: receipt.PublicID, Kind: "goods", Reason: "approved", EvidenceIDs: []string{evidence.PublicID}})
				require.NoError(t, err)
				fulfill := ShopFulfillInput{Command: ShopCommand{Now: 203}, OrderPublicID: order.PublicID, DeliveryPublicID: "live-delivery", StockUnitID: stock.ID, StockVersion: stock.Version}
				_, err = FulfillShopOrder(second, fulfill)
				assert.ErrorIs(t, err, ErrShopRevealBlocked)
				_, err = MutateShopRefund(db, ShopRefundActionInput{Command: shopTestCommand("live-refund-cancel", refund.Version, 204), RefundPublicID: refund.PublicID, Action: "cancel", Reason: "not operated", EvidenceIDs: []string{evidence.PublicID}})
				require.NoError(t, err)
				fulfill.Command.Now = 205
				delivery, err := FulfillShopOrder(second, fulfill)
				require.NoError(t, err)
				reveal := ShopRevealInput{Command: ShopCommand{ActorID: 1, TenantID: PlatformTenantID, Now: 206}, OrderPublicID: order.PublicID, DeliveryPublicID: delivery.PublicID, Intent: "view"}
				grant, err := RevealShopDelivery(db, reveal)
				require.NoError(t, err)
				assert.Equal(t, stock.SecretCiphertext, grant.Secret.Ciphertext)
				require.NoError(t, db.First(&receipt, receipt.ID).Error)
				_, err = ApproveShopRefund(second, ShopRefundApproveInput{Command: shopTestCommand("live-refund-reapprove", receipt.Version, 207), ReceiptPublicID: receipt.PublicID, Kind: "goods", Reason: "approved after delivery", EvidenceIDs: []string{evidence.PublicID}})
				require.NoError(t, err)
				grant, err = RevealShopDelivery(db, reveal)
				assert.ErrorIs(t, err, ErrShopRevealBlocked)
				assert.Empty(t, grant.Secret.Ciphertext)
			})
			t.Run("refund_committed_after_delivery_snapshot", func(t *testing.T) {
				db, second := shopLiveDatabase(t, dialect)
				order, receipt, stock := shopTestPaidInventory(t, db)
				evidence, err := RegisterShopPaymentEvidence(db, ShopEvidenceInput{Command: shopTestCommand("race-funds-evidence", receipt.Version, 201), ReceiptPublicID: receipt.PublicID, Evidence: ShopPaymentEvidence{Source: "merchant_console", ProviderTransactionID: receipt.ProviderTransactionID, AmountMinor: 100, Currency: "CNY", CurrencyExponent: 2, Reference: "merchant/transaction", Summary: "Not refunded", ObservedStatus: "not_refunded", ObservedAt: 201}})
				require.NoError(t, err)
				require.NoError(t, db.First(&receipt, receipt.ID).Error)
				read, resume := make(chan struct{}), make(chan struct{})
				var once sync.Once
				// Pause the delivery transaction after its initial order read. The
				// refund commits on the other session before delivery takes its locks.
				require.NoError(t, second.Callback().Query().After("gorm:query").Register("shop:test_order_snapshot", func(tx *gorm.DB) {
					if tx.Statement.Table == "shop_orders" {
						once.Do(func() { close(read); <-resume })
					}
				}))
				t.Cleanup(func() { assert.NoError(t, second.Callback().Query().Remove("shop:test_order_snapshot")) })
				result := make(chan error, 1)
				go func() {
					_, err := FulfillShopOrder(second, ShopFulfillInput{Command: ShopCommand{Now: 203}, OrderPublicID: order.PublicID, DeliveryPublicID: "racing-delivery", StockUnitID: stock.ID, StockVersion: stock.Version})
					result <- err
				}()
				<-read
				_, err = ApproveShopRefund(db, ShopRefundApproveInput{Command: shopTestCommand("race-refund-approve", receipt.Version, 202), ReceiptPublicID: receipt.PublicID, Kind: "goods", Reason: "approved before delivery lock", EvidenceIDs: []string{evidence.PublicID}})
				close(resume)
				deliveryErr := <-result
				require.NoError(t, err)
				assert.ErrorIs(t, deliveryErr, ErrShopRevealBlocked)
				var count int64
				require.NoError(t, db.Model(&ShopDelivery{}).Count(&count).Error)
				assert.Zero(t, count)
				require.NoError(t, db.First(&stock, stock.ID).Error)
				assert.Equal(t, "available", stock.State)
			})
			t.Run("leader_and_outbox_fence", func(t *testing.T) {
				db, second := shopLiveDatabase(t, dialect)
				order, attempt, _ := shopTestOrder(t, db)
				now := time.Now().Unix()
				leader := KKAIJobLease{LeaseName: "shop-live", Holder: "worker", Fence: 2, LeaseUntil: now + 3600, UpdatedAt: now}
				require.NoError(t, db.Create(&leader).Error)
				event := KKAIOutboxEvent{EventKey: "shop-live", Topic: ShopTopicReceipt, Status: KKAIOutboxStatusPending, LockedBy: "worker", LockedAt: now}
				require.NoError(t, db.Create(&event).Error)
				fence := ShopWorkerFence{EventID: event.ID, LockedBy: "worker", LockTimeout: time.Minute, LeaderName: leader.LeaseName, LeaderHolder: leader.Holder, LeaderFence: 2}
				require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
					if err := RequireShopWorkerFenceTx(tx, fence); err != nil {
						return err
					}
					lockErr := second.Transaction(func(contender *gorm.DB) error {
						statement := "SET LOCAL lock_timeout = '100ms'"
						if dialect == "mysql" {
							statement = "SET SESSION innodb_lock_wait_timeout = 1"
						}
						if err := contender.Exec(statement).Error; err != nil {
							return err
						}
						return contender.Model(&leader).Update("fence", 3).Error
					})
					if dialect == "mysql" {
						var failure *mysqldriver.MySQLError
						require.ErrorAs(t, lockErr, &failure)
						assert.EqualValues(t, 1205, failure.Number)
					} else {
						var failure *pgconn.PgError
						require.ErrorAs(t, lockErr, &failure)
						assert.Equal(t, "55P03", failure.Code)
					}
					return nil
				}))
				require.NoError(t, second.Model(&leader).Update("fence", 3).Error)
				assert.ErrorIs(t, HoldShopPaymentForReview(WithShopWorkerFence(db, fence), attempt.PublicID, "stale-leader", "proof", now), ErrShopWorkerFenceLost)
				fence.LeaderFence = 3
				require.NoError(t, HoldShopPaymentForReview(WithShopWorkerFence(db, fence), attempt.PublicID, "current-owner", "proof", now))
				require.NoError(t, second.Model(&event).Update("locked_by", "replacement").Error)
				assert.ErrorIs(t, HoldShopPaymentForReview(WithShopWorkerFence(db, fence), attempt.PublicID, "stale-outbox", "proof", now), ErrShopWorkerFenceLost)
				require.NoError(t, db.First(&order, order.ID).Error)
				assert.Equal(t, "current-owner", order.RiskReasonCode)
			})
		})
	}
}
