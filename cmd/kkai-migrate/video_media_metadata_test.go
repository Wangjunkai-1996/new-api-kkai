package main

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestVideoMediaMetadataProofBindsDatabasePlanAndRuntimeEvidence(t *testing.T) {
	rc49Proof, rc49Status, now := rc49ProofFixture(t)
	status := &kkaimigrate.VideoMediaMetadataExpansionStatus{
		Schema: "kkai_video_media_metadata_expansion_v1", Dialect: kkaimigrate.DialectPostgres,
		CurrentVersion: 21, TargetVersion: 21, SourceDigest: rc49Status.SourceDigest,
		DatabaseName: rc49Status.DatabaseName, DatabaseSchema: rc49Status.DatabaseSchema,
		PlanChecksum: kkaimigrate.VideoMediaMetadataPlanChecksum(), ExpansionDigest: kkaimigrate.VideoMediaMetadataExpansionDigest(), State: "pristine", Safe: true,
	}
	proof := videoMediaMetadataControllerProof{
		Schema: "kkai_video_media_metadata_expansion_proof_v1", Protocol: "video-media-metadata-v1", Operation: "apply",
		SourceVersion: 21, TargetVersion: 21, SourceDigest: status.SourceDigest, PlanChecksum: status.PlanChecksum, ExpansionDigest: status.ExpansionDigest,
		DatabaseName: status.DatabaseName, DatabaseSchema: status.DatabaseSchema,
		InstalledInfraSHA: rc49Proof.InstalledInfraSHA, ImageID: rc49Proof.ImageID, Nonce: rc49Proof.Nonce,
		IssuedAt: rc49Proof.IssuedAt, ExpiresAt: rc49Proof.ExpiresAt, Runtimes: rc49Proof.Runtimes,
	}
	require.NoError(t, validateVideoMediaMetadataControllerProof(&proof, status, now))
	raw, err := common.Marshal(proof)
	require.NoError(t, err)
	for index := range proof.Runtimes {
		delete(proof.Runtimes[index].SchemaContract.RequiredExpansions, kkaimigrate.VideoMediaMetadataExpansion)
	}
	require.NoError(t, validateVideoMediaMetadataControllerProof(&proof, status, now), "the previous canonical21+RC49 runtimes permit the additive expansion")
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
