package kkaimigrate

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newMigrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:kkai-migrate-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	return db
}

func TestApplyCreatesVersionedSchemaAndIsIdempotent(t *testing.T) {
	db := newMigrationTestDB(t)
	result, err := Apply(context.Background(), db, Options{})
	require.NoError(t, err)
	require.Len(t, result.Applied, int(RequiredRuntimeVersion))
	require.Empty(t, result.Pending)
	require.NoError(t, CheckRequired(context.Background(), db))
	require.True(t, db.Migrator().HasTable("kkai_policy_incidents"))
	require.True(t, db.Migrator().HasTable("kkai_outbox"))
	require.True(t, db.Migrator().HasTable("kkai_internal_balance_adjustments"))
	require.True(t, db.Migrator().HasTable("kkai_job_leases"))

	second, err := Apply(context.Background(), db, Options{})
	require.NoError(t, err)
	require.Len(t, second.Applied, int(RequiredRuntimeVersion))
	require.Empty(t, second.Pending)

	var count int64
	require.NoError(t, db.Model(&AppliedMigration{}).Count(&count).Error)
	require.EqualValues(t, RequiredRuntimeVersion, count)
}

func TestApplyDryRunDoesNotChangeSchema(t *testing.T) {
	db := newMigrationTestDB(t)
	result, err := Apply(context.Background(), db, Options{DryRun: true})
	require.NoError(t, err)
	require.Len(t, result.Pending, int(RequiredRuntimeVersion))
	require.False(t, db.Migrator().HasTable("kkai_schema_migrations"))
	require.False(t, db.Migrator().HasTable("kkai_policy_incidents"))
	require.False(t, db.Migrator().HasTable("kkai_outbox"))
	require.False(t, db.Migrator().HasTable("kkai_internal_balance_adjustments"))
	require.False(t, db.Migrator().HasTable("kkai_job_leases"))
}

func TestApplyVideoStudioExpandCreatesV5Schema(t *testing.T) {
	db := newMigrationTestDB(t)
	_, err := Apply(context.Background(), db, Options{})
	require.NoError(t, err)
	_, err = ApplyOutboxEventKeyCompatibility(context.Background(), db, Options{})
	require.NoError(t, err)

	result, err := ApplyVideoStudioExpand(context.Background(), db, Options{})
	require.NoError(t, err)
	require.Len(t, result.Applied, int(VideoStudioSchemaVersion))
	require.NoError(t, checkThroughVersion(
		context.Background(), db, VideoStudioSchemaVersion, VideoStudioSchemaVersion, MaxCompatibleVersion,
	))

	for _, table := range []string{
		"kkai_video_model_profiles",
		"kkai_video_samples",
		"kkai_video_generations",
		"kkai_video_assets",
		"kkai_video_task_assets",
		"kkai_idempotency_keys",
	} {
		require.True(t, db.Migrator().HasTable(table), table)
	}
}

func TestExecuteMigrationStatementSkipsExistingIndex(t *testing.T) {
	db := newMigrationTestDB(t)
	require.NoError(t, db.Exec("CREATE TABLE retryable_index (id INTEGER NOT NULL)").Error)
	statement := migrationStatement{
		Operation: migrationOperationCreateIndex,
		SQL:       "CREATE UNIQUE INDEX ux_retryable_index_id ON retryable_index (id)",
	}

	require.NoError(t, executeMigrationStatement(db, DialectSQLite, statement))
	require.NoError(t, executeMigrationStatement(db, DialectSQLite, statement))
	require.True(t, db.Migrator().HasIndex("retryable_index", "ux_retryable_index_id"))
}

func TestExecuteMigrationStatementSkipsExistingColumn(t *testing.T) {
	db := newMigrationTestDB(t)
	require.NoError(t, db.Exec("CREATE TABLE retryable_column (id INTEGER NOT NULL)").Error)
	statement := migrationStatement{
		Operation: migrationOperationAddNullableColumn,
		SQL:       "ALTER TABLE retryable_column ADD COLUMN category VARCHAR(32)",
	}

	require.NoError(t, executeMigrationStatement(db, DialectSQLite, statement))
	require.NoError(t, executeMigrationStatement(db, DialectSQLite, statement))
	exists, err := migrationColumnExists(db, "retryable_column", "category")
	require.NoError(t, err)
	require.True(t, exists)
}

func TestApplyVideoStudioExpandRequiresV4Bridge(t *testing.T) {
	db := newMigrationTestDB(t)
	_, err := applyThroughVersion(context.Background(), db, Options{}, JobLeaseSchemaVersion, MaxCompatibleVersion)
	require.NoError(t, err)

	_, err = ApplyVideoStudioExpand(context.Background(), db, Options{})
	require.ErrorIs(t, err, ErrSchemaNotReady)
	require.False(t, db.Migrator().HasTable("kkai_video_model_profiles"))
	var count int64
	require.NoError(t, db.Model(&AppliedMigration{}).Where("version > ?", JobLeaseSchemaVersion).Count(&count).Error)
	require.Zero(t, count)
}

func TestVideoStudioRuntimeSchemaRequiresMultipartAssetColumns(t *testing.T) {
	for _, column := range []string{"upload_mode", "multipart_upload_id", "upload_part_size"} {
		t.Run(column, func(t *testing.T) {
			db := newMigrationTestDB(t)
			_, err := Apply(context.Background(), db, Options{})
			require.NoError(t, err)
			_, err = ApplyOutboxEventKeyCompatibility(context.Background(), db, Options{})
			require.NoError(t, err)
			_, err = ApplyVideoStudioExpand(context.Background(), db, Options{})
			require.NoError(t, err)
			require.NoError(t, db.Exec("ALTER TABLE kkai_video_assets DROP COLUMN "+column).Error)

			err = checkThroughVersion(
				context.Background(), db, VideoStudioSchemaVersion, VideoStudioSchemaVersion, MaxCompatibleVersion,
			)
			require.ErrorIs(t, err, ErrSchemaNotReady)
		})
	}
}

func TestVideoStudioMaintenanceCanRunV4ThenV5(t *testing.T) {
	db := newMigrationTestDB(t)
	_, err := applyThroughVersion(context.Background(), db, Options{}, JobLeaseSchemaVersion, MaxCompatibleVersion)
	require.NoError(t, err)

	result, err := ApplyOutboxEventKeyCompatibility(context.Background(), db, Options{})
	require.NoError(t, err)
	require.Len(t, result.Applied, int(OutboxEventKeySchemaVersion))

	result, err = ApplyVideoStudioExpand(context.Background(), db, Options{})
	require.NoError(t, err)
	require.Len(t, result.Applied, int(VideoStudioSchemaVersion))
	require.NoError(t, checkThroughVersion(
		context.Background(), db, VideoStudioSchemaVersion, VideoStudioSchemaVersion, MaxCompatibleVersion,
	))
}

func TestApplyResumesFromValidAppliedPrefix(t *testing.T) {
	db := newMigrationTestDB(t)
	first, err := applyThroughVersion(context.Background(), db, Options{}, RiskSchemaVersion, MaxCompatibleVersion)
	require.NoError(t, err)
	require.Len(t, first.Applied, 1)

	var before AppliedMigration
	require.NoError(t, db.First(&before, "version = ?", RiskSchemaVersion).Error)
	result, err := Apply(context.Background(), db, Options{})
	require.NoError(t, err)
	require.Len(t, result.Applied, int(RequiredRuntimeVersion))

	var after AppliedMigration
	require.NoError(t, db.First(&after, "version = ?", RiskSchemaVersion).Error)
	require.Equal(t, before, after)
	var count int64
	require.NoError(t, db.Model(&AppliedMigration{}).Count(&count).Error)
	require.EqualValues(t, RequiredRuntimeVersion, count)
}

func TestApplyRejectsNonContiguousAppliedPrefix(t *testing.T) {
	db := newMigrationTestDB(t)
	_, err := applyThroughVersion(context.Background(), db, Options{}, RiskSchemaVersion, MaxCompatibleVersion)
	require.NoError(t, err)
	jobLease := migrationSet()[2]
	require.NoError(t, db.Create(&AppliedMigration{
		Version: jobLease.Version, Name: jobLease.Name, Checksum: storedMigrationChecksum(jobLease),
	}).Error)

	_, err = Apply(context.Background(), db, Options{})
	require.ErrorIs(t, err, ErrMigrationHole)
}

func TestCheckRejectsMissingAndTamperedMigrations(t *testing.T) {
	db := newMigrationTestDB(t)
	require.ErrorIs(t, Check(context.Background(), db, RiskSchemaVersion), ErrSchemaNotReady)
	_, err := Apply(context.Background(), db, Options{})
	require.NoError(t, err)
	require.NoError(t, db.Model(&AppliedMigration{}).Where("version = ?", RiskSchemaVersion).
		Update("checksum", "tampered").Error)
	require.ErrorIs(t, Check(context.Background(), db, CurrentVersion), ErrChecksumMismatch)
}

func TestCheckRejectsUnknownFutureMigration(t *testing.T) {
	db := newMigrationTestDB(t)
	_, err := Apply(context.Background(), db, Options{})
	require.NoError(t, err)
	require.NoError(t, db.Create(&AppliedMigration{
		Version:     CompatibleVersion + 1,
		Name:        "future",
		Checksum:    "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		AppliedAt:   1,
		ExecutionMS: 1,
	}).Error)
	require.ErrorIs(t, Check(context.Background(), db, CurrentVersion), ErrFutureMigration)
}

func TestCheckRejectsInvalidMigrationCatalog(t *testing.T) {
	db := newMigrationTestDB(t)
	invalid := []migration{{
		Version: 2, Name: "wrong_start", Kind: MigrationKindExpand,
		ImplementationID: "wrong_start_v1", ChecksumVersion: migrationChecksumSchemaCurrent,
		Statements: completeDialectStatements("CREATE TABLE wrong_start (id BIGINT)"),
	}}
	require.ErrorIs(
		t,
		checkThroughMigrationSet(context.Background(), db, 2, 2, 2, invalid),
		ErrUnsafeMigration,
	)
}

func TestCompatVersionAcceptsNewerKnownDatabaseWithoutApplyingIt(t *testing.T) {
	db := newMigrationTestDB(t)
	_, err := applyThroughVersion(context.Background(), db, Options{}, OutboxEventKeySchemaVersion, OutboxEventKeySchemaVersion)
	require.NoError(t, err)

	result, err := applyThroughVersion(context.Background(), db, Options{}, JobLeaseSchemaVersion, OutboxEventKeySchemaVersion)
	require.NoError(t, err)
	require.Len(t, result.Applied, 3)
	require.Empty(t, result.Pending)
	require.NoError(t, checkThroughVersion(
		context.Background(),
		db,
		JobLeaseSchemaVersion,
		JobLeaseSchemaVersion,
		OutboxEventKeySchemaVersion,
	))
}

func TestCompatVersionApplyStopsAtCurrentVersion(t *testing.T) {
	db := newMigrationTestDB(t)
	result, err := applyThroughVersion(context.Background(), db, Options{}, JobLeaseSchemaVersion, OutboxEventKeySchemaVersion)
	require.NoError(t, err)
	require.Len(t, result.Applied, 3)
	require.Empty(t, result.Pending)

	var count int64
	require.NoError(t, db.Model(&AppliedMigration{}).Where("version = ?", OutboxEventKeySchemaVersion).Count(&count).Error)
	require.Zero(t, count)
}

type testLegacyPolicyIncident struct {
	ID                     int64  `gorm:"primaryKey"`
	RequestID              string `gorm:"column:request_id"`
	UserID                 int    `gorm:"column:user_id"`
	TokenID                int    `gorm:"column:token_id"`
	TokenName              string `gorm:"column:token_name"`
	ModelName              string `gorm:"column:model_name"`
	ChannelID              int    `gorm:"column:channel_id"`
	UpstreamKeyFingerprint string `gorm:"column:upstream_key_fingerprint"`
	EvidenceLevel          string `gorm:"column:evidence_level"`
	Causality              string `gorm:"column:causality"`
	ActionTaken            string `gorm:"column:action_taken"`
	ActionResult           string `gorm:"column:action_result"`
	Metadata               string `gorm:"column:metadata;type:text"`
	CreatedAt              int64  `gorm:"column:created_at"`
}

func (testLegacyPolicyIncident) TableName() string { return "policy_incident_events" }

type testLegacyBalanceAdjustment struct {
	ID                  int64   `gorm:"primaryKey"`
	OperationID         string  `gorm:"column:operation_id"`
	UserID              int     `gorm:"column:user_id"`
	Delta               int64   `gorm:"column:delta"`
	Reason              string  `gorm:"column:reason"`
	Metadata            string  `gorm:"column:metadata;type:text"`
	PayloadSHA256       string  `gorm:"column:payload_sha256"`
	OriginalOperationID *string `gorm:"column:original_operation_id"`
	BalanceBefore       int64   `gorm:"column:balance_before"`
	BalanceAfter        int64   `gorm:"column:balance_after"`
	CreatedAt           int64   `gorm:"column:created_at"`
}

func (testLegacyBalanceAdjustment) TableName() string { return "internal_balance_adjustments" }

func TestApplyImportsLegacyAuditRowsWithoutReplayingActions(t *testing.T) {
	db := newMigrationTestDB(t)
	require.NoError(t, db.AutoMigrate(&testLegacyPolicyIncident{}, &testLegacyBalanceAdjustment{}))
	legacyPolicy := testLegacyPolicyIncident{
		ID:                     11,
		RequestID:              "legacy-request",
		UserID:                 7,
		TokenID:                8,
		TokenName:              "must-not-be-copied",
		ModelName:              "legacy-model",
		ChannelID:              9,
		UpstreamKeyFingerprint: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		EvidenceLevel:          "confirmed",
		Causality:              "client_token",
		ActionTaken:            "token_disabled,user_disabled",
		ActionResult:           "success,success",
		Metadata:               `{"case_id":"legacy-case"}`,
		CreatedAt:              1_700_000_000,
	}
	require.NoError(t, db.Create(&legacyPolicy).Error)
	legacyBalance := testLegacyBalanceAdjustment{
		ID:            12,
		OperationID:   "invite:legacy:12",
		UserID:        7,
		Delta:         100,
		Reason:        "invitation_reward",
		Metadata:      `{"rebate_record_id":12}`,
		PayloadSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		BalanceBefore: 200,
		BalanceAfter:  300,
		CreatedAt:     1_700_000_001,
	}
	require.NoError(t, db.Create(&legacyBalance).Error)

	_, err := Apply(context.Background(), db, Options{})
	require.NoError(t, err)

	var incident model.KKAIPolicyIncident
	require.NoError(t, db.Where("event_id = ?", "legacy-policy-incident:11").First(&incident).Error)
	require.NotContains(t, incident.Metadata, legacyPolicy.TokenName)
	require.True(t, incident.TokenDisabled)
	require.True(t, incident.UserDisabled)

	var adjustment model.KKAIInternalBalanceAdjustment
	require.NoError(t, db.Where("operation_id = ?", legacyBalance.OperationID).First(&adjustment).Error)
	require.Equal(t, legacyBalance.BalanceBefore, adjustment.BalanceBefore)
	require.Equal(t, legacyBalance.BalanceAfter, adjustment.BalanceAfter)

	require.True(t, db.Migrator().HasTable("policy_incident_events"))
	require.True(t, db.Migrator().HasTable("internal_balance_adjustments"))
}

func TestPlanHasImmutableChecksums(t *testing.T) {
	require.Equal(t, []AppliedMigration{
		{
			Version:  RiskSchemaVersion,
			Name:     "risk_incidents_and_outbox",
			Checksum: "96efb7eaeb9be70f3f9feba02ba68ac31aa55a61c026645c249aa4c87fb323ae",
		},
		{
			Version:  LedgerSchemaVersion,
			Name:     "internal_balance_ledger",
			Checksum: "28be60ae8ec61dde922cc726be1073fa795e7de33ff662dc30ebf731ac25a8d1",
		},
		{
			Version:  JobLeaseSchemaVersion,
			Name:     "background_job_leases",
			Checksum: "cdd37df49c8171159556679f8733cda0301256a290653bd9f0e9fdf8c2029a6f",
		},
		{
			Version:  OutboxEventKeySchemaVersion,
			Name:     "outbox_event_key_mysql57_compat",
			Checksum: "453307264b9eabffe35597460ea35c60372eb40dcb7cf1bf5ae7e696a3eb92df",
		},
		{
			Version:  VideoStudioSchemaVersion,
			Name:     "video_studio",
			Checksum: "ca0fcda4889bcaa6d0d0dbf37fef1f61402bb3a04a89e14e86bf76cec32287d4",
		},
		{
			Version:  VideoSampleCategorySchemaVersion,
			Name:     "video_sample_category",
			Checksum: "1345358ef3e7bfcab21fb54716f529befa61b393edc3e6478a03d29235787899",
		},
		{
			Version:  ImageStudioSchemaVersion,
			Name:     "image_studio",
			Checksum: "77c7cf3097c592a04f0e59ffab99ee48a74a733f2e697a4ee7265d1eff512048",
		},
	}, Plan())
}

func TestApplySerializesConcurrentCallers(t *testing.T) {
	db := newMigrationTestDB(t)
	const workers = 8
	var wg sync.WaitGroup
	errorsByWorker := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := Apply(context.Background(), db, Options{})
			errorsByWorker <- err
		}()
	}
	wg.Wait()
	close(errorsByWorker)
	for err := range errorsByWorker {
		require.NoError(t, err)
	}

	var count int64
	require.NoError(t, db.Model(&AppliedMigration{}).Count(&count).Error)
	require.EqualValues(t, RequiredRuntimeVersion, count)
}
