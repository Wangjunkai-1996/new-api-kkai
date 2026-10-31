package kkaimigrate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestImageAsyncRuntimeSchemaIgnoresUnrelatedPhysicalDamage(t *testing.T) {
	db := newMigrationTestDB(t)
	ctx := context.Background()
	_, err := applyThroughVersion(ctx, db, Options{}, ImageAsyncSchemaVersion, KnownCatalogVersion)
	require.NoError(t, err)
	require.NoError(t, CheckImageAsyncRuntimeSchema(ctx, db))
	require.NoError(t, db.Migrator().DropTable("kkai_policy_incidents"))
	assert.ErrorIs(t, CheckImageAsyncSchema(ctx, db), ErrSchemaNotReady)
	assert.NoError(t, CheckImageAsyncRuntimeSchema(ctx, db))
}

func TestImageAsyncRuntimeSchemaRejectsMissingDependencies(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*gorm.DB) error
		wanted error
	}{
		{"missing migration receipt", func(db *gorm.DB) error {
			return db.Where("version = ?", ImageAsyncSchemaVersion).Delete(&AppliedMigration{}).Error
		}, ErrSchemaNotReady},
		{"migration receipt", func(db *gorm.DB) error {
			return db.Model(&AppliedMigration{}).Where("version = ?", ImageAsyncSchemaVersion).Update("checksum", "invalid").Error
		}, ErrChecksumMismatch},
		{"generation request uniqueness", func(db *gorm.DB) error {
			return db.Migrator().DropIndex("kkai_image_generations", "ux_kkai_image_generations_request")
		}, ErrSchemaNotReady},
		{"reference upload uniqueness", func(db *gorm.DB) error {
			return db.Migrator().DropIndex("kkai_image_reference_uploads", "ux_image_reference_upload_key")
		}, ErrSchemaNotReady},
		{"execution payload", func(db *gorm.DB) error {
			return db.Exec("ALTER TABLE kkai_image_executions DROP COLUMN payload").Error
		}, ErrSchemaNotReady},
		{"outbox table", func(db *gorm.DB) error {
			return db.Migrator().DropTable("kkai_outbox")
		}, ErrSchemaNotReady},
		{"worker leases", func(db *gorm.DB) error {
			return db.Migrator().DropTable("kkai_job_leases")
		}, ErrSchemaNotReady},
		{"model profiles", func(db *gorm.DB) error {
			return db.Migrator().DropTable("kkai_image_model_profiles")
		}, ErrSchemaNotReady},
		{"samples", func(db *gorm.DB) error {
			return db.Migrator().DropTable("kkai_image_samples")
		}, ErrSchemaNotReady},
		{"model uniqueness", func(db *gorm.DB) error {
			return db.Migrator().DropIndex("kkai_image_model_profiles", "ux_kkai_image_model_profiles_model")
		}, ErrSchemaNotReady},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newMigrationTestDB(t)
			_, err := applyThroughVersion(context.Background(), db, Options{}, ImageAsyncSchemaVersion, KnownCatalogVersion)
			require.NoError(t, err)
			require.NoError(t, CheckImageAsyncRuntimeSchema(context.Background(), db))
			require.NoError(t, tc.mutate(db))
			assert.ErrorIs(t, CheckImageAsyncRuntimeSchema(context.Background(), db), tc.wanted)
		})
	}
}

func TestImageAsyncRuntimeSchemaAcceptsLaterCanonicalLedger(t *testing.T) {
	db := newMigrationTestDB(t)
	ctx := context.Background()
	_, err := applyThroughVersion(ctx, db, Options{}, MembershipSchemaVersion, KnownCatalogVersion)
	require.NoError(t, err)
	require.NoError(t, CheckImageAsyncRuntimeSchema(ctx, db))
	require.NoError(t, db.Migrator().DropTable("kkai_membership_orders"))
	assert.NoError(t, CheckImageAsyncRuntimeSchema(ctx, db))
}

func TestImageAsyncRuntimeSchemaAcceptsShopLedgerWithoutShopSchema(t *testing.T) {
	db := newMigrationTestDB(t)
	ctx := context.Background()
	options := prepareRequestTicketExpandFixture(t, db)
	_, err := ApplyRequestTicketExpand(ctx, db, options)
	require.NoError(t, err)
	_, err = ApplyInvoiceExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	for _, ddl := range []string{"ALTER TABLE top_ups ADD COLUMN user_id INTEGER NULL", "ALTER TABLE top_ups ADD COLUMN create_time INTEGER NULL"} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	_, err = ApplyWalletPaymentExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	_, err = ApplyShopExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	require.NoError(t, CheckImageAsyncRuntimeSchema(ctx, db))
	require.NoError(t, db.Migrator().DropTable("shop_orders"))
	assert.NoError(t, CheckImageAsyncRuntimeSchema(ctx, db))
}
