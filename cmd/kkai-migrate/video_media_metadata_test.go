package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestVideoMediaMetadataCommandRequiresProofOnlyForApply(t *testing.T) {
	for _, operation := range []string{"plan", "status", "check"} {
		require.NoError(t, validateVideoMediaMetadataCommand(operation, "", true, false))
		assert.Error(t, validateVideoMediaMetadataCommand(operation, videoMediaMetadataControllerProofPath, true, false))
		assert.Error(t, validateVideoMediaMetadataCommand(operation, "", false, false))
	}
	require.NoError(t, validateVideoMediaMetadataCommand("apply", videoMediaMetadataControllerProofPath, true, false))
	assert.Error(t, validateVideoMediaMetadataCommand("apply", "", true, false))
	assert.Error(t, validateVideoMediaMetadataCommand("apply", videoMediaMetadataControllerProofPath, true, true))
	assert.Error(t, validateVideoMediaMetadataCommand("finalize", videoMediaMetadataControllerProofPath, true, false))
}

func videoMediaMetadataProofFixture(t *testing.T) (*videoMediaMetadataControllerProof, *kkaimigrate.VideoMediaMetadataExpansionStatus, time.Time) {
	t.Helper()
	rc49Proof, rc49Status, now := rc49ProofFixture(t)
	status := &kkaimigrate.VideoMediaMetadataExpansionStatus{
		Schema: "kkai_video_media_metadata_expansion_v1", Dialect: kkaimigrate.DialectPostgres,
		CurrentVersion: 21, TargetVersion: 21, SourceDigest: rc49Status.SourceDigest,
		DatabaseName: rc49Status.DatabaseName, DatabaseSchema: rc49Status.DatabaseSchema,
		PlanChecksum: kkaimigrate.VideoMediaMetadataPlanChecksum(), ExpansionDigest: kkaimigrate.VideoMediaMetadataExpansionDigest(), State: "pristine", Safe: true,
	}
	proof := &videoMediaMetadataControllerProof{
		Schema: "kkai_video_media_metadata_expansion_proof_v1", Protocol: "video-media-metadata-v1", Operation: "apply",
		SourceVersion: 21, TargetVersion: 21, SourceDigest: status.SourceDigest, PlanChecksum: status.PlanChecksum, ExpansionDigest: status.ExpansionDigest,
		DatabaseName: status.DatabaseName, DatabaseSchema: status.DatabaseSchema,
		InstalledInfraSHA: rc49Proof.InstalledInfraSHA, ImageID: rc49Proof.ImageID, Nonce: rc49Proof.Nonce,
		IssuedAt: rc49Proof.IssuedAt, ExpiresAt: rc49Proof.ExpiresAt, Runtimes: rc49Proof.Runtimes,
	}
	return proof, status, now
}

func TestVideoMediaMetadataProofBindsDatabasePlanAndRuntimeEvidence(t *testing.T) {
	proof, status, now := videoMediaMetadataProofFixture(t)
	require.NoError(t, validateVideoMediaMetadataControllerProof(proof, status, now))
	raw, err := common.Marshal(proof)
	require.NoError(t, err)
	for index := range proof.Runtimes {
		delete(proof.Runtimes[index].SchemaContract.RequiredExpansions, kkaimigrate.VideoMediaMetadataExpansion)
	}
	require.NoError(t, validateVideoMediaMetadataControllerProof(proof, status, now), "the previous canonical21+RC49 runtimes permit the additive expansion")
	for _, test := range []struct {
		name string
		edit func(*videoMediaMetadataControllerProof)
	}{
		{"wrong protocol", func(p *videoMediaMetadataControllerProof) { p.Protocol = "rc49-expansion-v1" }},
		{"wrong plan", func(p *videoMediaMetadataControllerProof) { p.PlanChecksum = "wrong" }},
		{"wrong database", func(p *videoMediaMetadataControllerProof) { p.DatabaseName = "other" }},
		{"wrong expansion", func(p *videoMediaMetadataControllerProof) { p.ExpansionDigest = "wrong" }},
		{"expired", func(p *videoMediaMetadataControllerProof) { p.ExpiresAt = now.Unix() }},
		{"missing writer", func(p *videoMediaMetadataControllerProof) { p.Runtimes = p.Runtimes[:2] }},
		{"duplicate process", func(p *videoMediaMetadataControllerProof) {
			p.Runtimes[1].ApplicationPID = p.Runtimes[0].ApplicationPID
		}},
		{"incompatible runtime", func(p *videoMediaMetadataControllerProof) { p.Runtimes[1].SchemaContract.RequiredExpansions = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			var invalid videoMediaMetadataControllerProof
			require.NoError(t, common.Unmarshal(raw, &invalid))
			test.edit(&invalid)
			assert.Error(t, validateVideoMediaMetadataControllerProof(&invalid, status, now))
		})
	}
}

func TestVideoMediaMetadataPostgresLockedProofRechecksIdentityAndExpiry(t *testing.T) {
	dsn := os.Getenv("KKAI_VIDEO_MEDIA_GUARD_TEST_DSN")
	if dsn == "" {
		t.Skip("KKAI_VIDEO_MEDIA_GUARD_TEST_DSN is not set")
	}
	config, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1", config.Host)
	require.True(t, strings.HasPrefix(config.Database, "kkai_test_video_media_guard_"))
	require.Empty(t, config.Fallbacks)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	connection, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	proof, status, _ := videoMediaMetadataProofFixture(t)
	now := time.Now()
	proof.IssuedAt, proof.ExpiresAt = now.Unix()-1, now.Unix()+119
	for index := range proof.Runtimes {
		proof.Runtimes[index].ContainerStartedAt = now.Add(-time.Minute).Format(time.RFC3339Nano)
	}
	require.NoError(t, db.WithContext(t.Context()).Transaction(func(locked *gorm.DB) error {
		require.NoError(t, locked.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "kkai_schema_migrations").Error)
		name, schema, err := kkaimigrate.TenantOnlineDatabaseIdentity(t.Context(), locked)
		require.NoError(t, err)
		status.DatabaseName, status.DatabaseSchema = name, schema
		proof.DatabaseName, proof.DatabaseSchema = name, schema
		require.NoError(t, authorizeVideoMediaMetadataMutation(t.Context(), locked, proof, status))
		proof.ExpiresAt = now.Unix()
		assert.ErrorContains(t, authorizeVideoMediaMetadataMutation(t.Context(), locked, proof, status), "expired")
		proof.ExpiresAt = now.Unix() + 119
		proof.PlanChecksum = strings.Repeat("0", 64)
		assert.ErrorContains(t, authorizeVideoMediaMetadataMutation(t.Context(), locked, proof, status), "exact database and expansion plan")
		proof.PlanChecksum = status.PlanChecksum
		require.NoError(t, locked.Exec("SET LOCAL search_path = pg_catalog").Error)
		assert.ErrorContains(t, authorizeVideoMediaMetadataMutation(t.Context(), locked, proof, status), "database identity changed")
		return nil
	}))
}
