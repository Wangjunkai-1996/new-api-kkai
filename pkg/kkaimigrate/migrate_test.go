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
	require.Len(t, result.Applied, 4)
	require.Empty(t, result.Pending)
	require.NoError(t, Check(context.Background(), db, MigrationTargetVersion))
	require.True(t, db.Migrator().HasTable("kkai_policy_incidents"))
	require.True(t, db.Migrator().HasTable("kkai_outbox"))
	require.True(t, db.Migrator().HasTable("kkai_internal_balance_adjustments"))
	require.True(t, db.Migrator().HasTable("kkai_job_leases"))

	second, err := Apply(context.Background(), db, Options{})
	require.NoError(t, err)
	require.Len(t, second.Applied, 4)
	require.Empty(t, second.Pending)

	var count int64
	require.NoError(t, db.Model(&AppliedMigration{}).Count(&count).Error)
	require.EqualValues(t, 4, count)
}

func TestApplyDryRunDoesNotChangeSchema(t *testing.T) {
	db := newMigrationTestDB(t)
	result, err := Apply(context.Background(), db, Options{DryRun: true})
	require.NoError(t, err)
	require.Len(t, result.Pending, 4)
	require.False(t, db.Migrator().HasTable("kkai_schema_migrations"))
	require.False(t, db.Migrator().HasTable("kkai_policy_incidents"))
	require.False(t, db.Migrator().HasTable("kkai_outbox"))
	require.False(t, db.Migrator().HasTable("kkai_internal_balance_adjustments"))
	require.False(t, db.Migrator().HasTable("kkai_job_leases"))
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

func TestMigrationTargetControlsBridgeAndExpandApplication(t *testing.T) {
	migrations := append(migrationSet(), migration{
		Version:          5,
		Name:             "synthetic_expand",
		Kind:             MigrationKindExpand,
		ImplementationID: "synthetic_expand_v1",
		ChecksumVersion:  migrationChecksumSchemaCurrent,
		Statements: completeDialectOperations(
			migrationStatement{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE kkai_synthetic_expand (id INTEGER PRIMARY KEY)`},
			migrationStatement{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_kkai_synthetic_expand_id ON kkai_synthetic_expand (id)`},
		),
	})

	for _, test := range []struct {
		name            string
		targetVersion   int64
		expectsVersion5 bool
	}{
		{name: "bridge does not apply the next expansion", targetVersion: 4, expectsVersion5: false},
		{name: "expand applies the next expansion", targetVersion: 5, expectsVersion5: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := newMigrationTestDB(t)
			_, err := applyThroughMigrationSet(context.Background(), db, Options{}, 4, 5, migrations)
			require.NoError(t, err)

			result, err := applyThroughMigrationSet(context.Background(), db, Options{}, test.targetVersion, 5, migrations)
			require.NoError(t, err)
			require.Len(t, result.Applied, int(test.targetVersion))
			require.Empty(t, result.Pending)
			require.NoError(t, checkThroughMigrationSet(context.Background(), db, test.targetVersion, 4, 5, migrations))

			var appliedVersion5 int64
			require.NoError(t, db.Model(&AppliedMigration{}).Where("version = ?", 5).Count(&appliedVersion5).Error)
			require.Equal(t, test.expectsVersion5, appliedVersion5 == 1)
			require.Equal(t, test.expectsVersion5, db.Migrator().HasIndex("kkai_synthetic_expand", "idx_kkai_synthetic_expand_id"))
		})
	}
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
	require.EqualValues(t, 4, count)
}
