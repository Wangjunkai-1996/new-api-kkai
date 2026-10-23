package model

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTopUpLegacyPostgresInsertSerializesWithWalletDDL(t *testing.T) {
	db, migration, _ := resellerPostgresAcceptanceDB(t)
	for _, column := range walletTopUpColumnNames {
		require.NoError(t, db.Migrator().DropColumn(&TopUp{}, column))
	}
	user := User{Username: "legacy-before-expansion", AffCode: "legacy-before-expansion"}
	require.NoError(t, db.Create(&user).Error)
	ready, release := make(chan struct{}), make(chan struct{})
	finished := make(chan error, 1)
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	order := &TopUp{UserId: user.Id, TradeNo: "before-wallet-ddl", PaymentProvider: PaymentProviderEpay}
	go func() {
		finished <- order.InsertBeforeWalletPaymentExpand(context.Background(), func(ctx context.Context, tx *gorm.DB) error {
			if tx.Migrator().HasColumn("top_ups", "wallet_lifecycle_version") {
				return ErrWalletPaymentSchema
			}
			close(ready)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-ready:
	case err := <-finished:
		t.Fatalf("legacy insert ended before entering the schema check: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("legacy insert did not reach the schema check")
	}
	// PostgreSQL DDL needs ACCESS EXCLUSIVE. A schema check on unrelated
	// catalog tables does not hold that lock; the explicit top_ups read does.
	err := migration.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL lock_timeout = '150ms'").Error; err != nil {
			return err
		}
		return tx.Exec("ALTER TABLE top_ups ADD COLUMN wallet_lifecycle_version BIGINT NULL").Error
	})
	var postgresError *pgconn.PgError
	require.ErrorAs(t, err, &postgresError)
	assert.Equal(t, "55P03", postgresError.Code, "DDL must wait for the legacy purchase transaction")
	close(release)
	require.NoError(t, <-finished)
	var count int64
	require.NoError(t, db.Model(&TopUp{}).Where("trade_no = ?", order.TradeNo).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	require.NoError(t, migration.Exec("ALTER TABLE top_ups ADD COLUMN wallet_lifecycle_version BIGINT NULL").Error)
	late := &TopUp{UserId: user.Id, TradeNo: "after-wallet-ddl"}
	err = late.InsertBeforeWalletPaymentExpand(context.Background(), func(_ context.Context, tx *gorm.DB) error {
		if tx.Migrator().HasColumn("top_ups", "wallet_lifecycle_version") {
			return ErrWalletPaymentSchema
		}
		return nil
	})
	assert.ErrorIs(t, err, ErrWalletPaymentSchema)
	require.NoError(t, db.Model(&TopUp{}).Where("trade_no = ?", late.TradeNo).Count(&count).Error)
	assert.Zero(t, count, "a purchase that observes the new schema cannot insert a legacy row")
}
