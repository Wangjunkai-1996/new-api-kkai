package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/imagepricing"
	"github.com/QuantumNous/new-api/setting/image_studio_setting"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestNormalizeImageStudioSubmissionMergesDefaultsAndBuildsStrictRelayRequest(t *testing.T) {
	db, profile := newImageSubmissionTestDB(t)
	request := ImageStudioSubmissionRequest{
		TokenID: 4, Model: profile.Model, Prompt: "  a lighthouse  ",
		Parameters: map[string]any{"count": float64(2)},
	}

	normalized, err := NormalizeImageStudioSubmission(context.Background(), db, 7, request)
	require.NoError(t, err)
	assert.Equal(t, "a lighthouse", normalized.Prompt)
	assert.Equal(t, 2, normalized.RequestedCount)
	assert.Equal(t, uint(2), *normalized.RelayRequest.N)
	assert.Equal(t, "1024x1024", normalized.RelayRequest.Size)
	require.NotNil(t, normalized.RelayRequest.Stream)
	assert.False(t, *normalized.RelayRequest.Stream)
	assert.Empty(t, normalized.RelayRequest.Extra)
	assert.Len(t, normalized.RequestHash, 64)
}

func TestImageStudioOutputCountChangesRequestHashAndConflictsOnIdempotencyKey(t *testing.T) {
	db, profile := newImageSubmissionTestDB(t)
	requests := make(map[int]*NormalizedImageStudioSubmission, 2)
	for _, count := range []int{1, 4} {
		normalized, err := NormalizeImageStudioSubmission(
			context.Background(), db, 7, ImageStudioSubmissionRequest{
				TokenID: 4, Model: profile.Model, Prompt: "the same prompt",
				Parameters: map[string]any{"count": count},
			},
		)
		require.NoError(t, err)
		requests[count] = normalized
	}
	require.NotEqual(t, requests[1].RequestHash, requests[4].RequestHash)

	_, err := ReserveIdempotencyKey(context.Background(), db, IdempotencyReservationRequest{
		UserID: 7, Operation: model.ImageIdempotencyOperationSubmit,
		Key: "same-key-different-count", RequestHash: requests[1].RequestHash,
	})
	require.NoError(t, err)
	_, err = ReserveIdempotencyKey(context.Background(), db, IdempotencyReservationRequest{
		UserID: 7, Operation: model.ImageIdempotencyOperationSubmit,
		Key: "same-key-different-count", RequestHash: requests[4].RequestHash,
	})
	assert.ErrorIs(t, err, ErrIdempotencyConflict)
}

func TestImageStudioGenerationRequestHashRemainsBackwardCompatible(t *testing.T) {
	normalized := &NormalizedImageStudioSubmission{
		TokenID: 4, ProfileID: 8, SpecificationVersion: 3,
		Model: "gpt-image-2", Prompt: "a lighthouse",
		Parameters: map[string]any{"size": "1024x1024", "count": 2},
		Mode:       ImageStudioModeGeneration,
	}
	specification := ImageModelSpec{Parameters: []ImageParameterSpec{
		{Key: "size", RequestKey: "size"},
		{Key: "count", RequestKey: "n"},
	}}

	requestHash, err := imageStudioRequestHash(normalized, specification)
	require.NoError(t, err)
	legacyCanonical := []byte(`{"token_id":4,"profile_id":8,"specification_version":3,"model":"gpt-image-2","prompt":"a lighthouse","parameters":[{"key":"size","request_key":"size","value":"1024x1024"},{"key":"count","request_key":"n","value":2}]}`)
	digest := sha256.Sum256(legacyCanonical)
	assert.Equal(t, hex.EncodeToString(digest[:]), requestHash)
}

func TestNormalizeImageStudioEditBindsOrderedReferencesAndModelLimit(t *testing.T) {
	db, profile := newImageSubmissionTestDB(t)
	require.NoError(t, db.Model(&profile).Update("model", ImageStudioEditModel).Error)
	profile.Model = ImageStudioEditModel
	specification := imageSubmissionSpec()
	specification.MaxReferenceImages = 2
	encodedSpecification, err := common.Marshal(specification)
	require.NoError(t, err)
	require.NoError(t, db.Model(&profile).Updates(map[string]any{
		"specification": string(encodedSpecification), "specification_version": specification.Version,
	}).Error)
	references := []ImageStudioReferenceMetadata{
		{SHA256: strings.Repeat("A", 64), SizeBytes: 1234},
		{SHA256: strings.Repeat("B", 64), SizeBytes: 5678},
	}
	request := ImageStudioSubmissionRequest{
		TokenID: 4, Model: profile.Model, Prompt: "edit the lighthouse",
		Mode: ImageStudioModeEdit, References: references,
	}

	normalized, err := NormalizeImageStudioSubmission(context.Background(), db, 7, request)
	require.NoError(t, err)
	assert.Equal(t, ImageStudioModeEdit, normalized.Mode)
	require.Len(t, normalized.References, 2)
	assert.Equal(t, strings.Repeat("a", 64), normalized.References[0].SHA256)
	assert.Equal(t, strings.Repeat("b", 64), normalized.References[1].SHA256)
	assert.EqualValues(t, 1234, normalized.References[0].SizeBytes)
	assert.EqualValues(t, 5678, normalized.References[1].SizeBytes)
	assert.NotSame(t, &references[0], &normalized.References[0])

	batchEdit := request
	batchEdit.Parameters = map[string]any{"count": 2}
	_, err = NormalizeImageStudioSubmission(context.Background(), db, 7, batchEdit)
	assert.ErrorIs(t, err, ErrInvalidImageStudioSubmission)

	generation, err := NormalizeImageStudioSubmission(context.Background(), db, 7, ImageStudioSubmissionRequest{
		TokenID: 4, Model: profile.Model, Prompt: "edit the lighthouse",
	})
	require.NoError(t, err)
	assert.NotEqual(t, generation.RequestHash, normalized.RequestHash)

	changed := request
	changed.References = append([]ImageStudioReferenceMetadata(nil), references...)
	changed.References[1].SHA256 = strings.Repeat("c", 64)
	changedNormalized, err := NormalizeImageStudioSubmission(context.Background(), db, 7, changed)
	require.NoError(t, err)
	assert.NotEqual(t, normalized.RequestHash, changedNormalized.RequestHash)

	reordered := request
	reordered.References = []ImageStudioReferenceMetadata{references[1], references[0]}
	reorderedNormalized, err := NormalizeImageStudioSubmission(context.Background(), db, 7, reordered)
	require.NoError(t, err)
	assert.NotEqual(t, normalized.RequestHash, reorderedNormalized.RequestHash)
	_, err = ReserveIdempotencyKey(context.Background(), db, IdempotencyReservationRequest{
		UserID: 7, Operation: model.ImageIdempotencyOperationSubmit,
		Key: "ordered-reference-submit", RequestHash: normalized.RequestHash,
	})
	require.NoError(t, err)
	_, err = ReserveIdempotencyKey(context.Background(), db, IdempotencyReservationRequest{
		UserID: 7, Operation: model.ImageIdempotencyOperationSubmit,
		Key: "ordered-reference-submit", RequestHash: reorderedNormalized.RequestHash,
	})
	assert.ErrorIs(t, err, ErrIdempotencyConflict)

	invalid := []ImageStudioSubmissionRequest{
		{TokenID: 4, Model: "gpt-image-2k", Prompt: "edit", Mode: ImageStudioModeEdit, References: references},
		{TokenID: 4, Model: profile.Model, Prompt: "edit", Mode: ImageStudioModeEdit},
		{TokenID: 4, Model: profile.Model, Prompt: "edit", Mode: ImageStudioModeEdit, References: []ImageStudioReferenceMetadata{{SHA256: "invalid", SizeBytes: 1}}},
		{TokenID: 4, Model: profile.Model, Prompt: "edit", Mode: ImageStudioModeEdit, References: []ImageStudioReferenceMetadata{{SHA256: strings.Repeat("a", 64)}}},
		{TokenID: 4, Model: profile.Model, Prompt: "edit", Mode: ImageStudioModeEdit, References: append(references, references[0])},
		{TokenID: 4, Model: profile.Model, Prompt: "generate", References: references},
	}
	for _, candidate := range invalid {
		_, err := NormalizeImageStudioSubmission(context.Background(), db, 7, candidate)
		assert.ErrorIs(t, err, ErrInvalidImageStudioSubmission)
	}
}

func TestNormalizeImageStudioEditDefaultsToOneReferenceAndEnforcesByteLimits(t *testing.T) {
	db, profile := newImageSubmissionTestDB(t)
	require.NoError(t, db.Model(&profile).Update("model", ImageStudioEditModel).Error)
	profile.Model = ImageStudioEditModel
	settings := image_studio_setting.Get()
	reference := ImageStudioReferenceMetadata{SHA256: strings.Repeat("a", 64), SizeBytes: settings.MaxReferenceBytes}

	_, err := NormalizeImageStudioSubmission(context.Background(), db, 7, ImageStudioSubmissionRequest{
		TokenID: 4, Model: profile.Model, Prompt: "edit", Mode: ImageStudioModeEdit,
		References: []ImageStudioReferenceMetadata{reference},
	})
	require.NoError(t, err)

	_, err = NormalizeImageStudioSubmission(context.Background(), db, 7, ImageStudioSubmissionRequest{
		TokenID: 4, Model: profile.Model, Prompt: "edit", Mode: ImageStudioModeEdit,
		References: []ImageStudioReferenceMetadata{reference, reference},
	})
	assert.ErrorIs(t, err, ErrInvalidImageStudioSubmission)

	tooLarge := reference
	tooLarge.SizeBytes++
	_, err = NormalizeImageStudioSubmission(context.Background(), db, 7, ImageStudioSubmissionRequest{
		TokenID: 4, Model: profile.Model, Prompt: "edit", Mode: ImageStudioModeEdit,
		References: []ImageStudioReferenceMetadata{tooLarge},
	})
	assert.ErrorIs(t, err, ErrInvalidImageStudioSubmission)
}

func TestImageStudioEditQuoteRejectsReorderedUploadedReferences(t *testing.T) {
	db, profile := newImageSubmissionTestDB(t)
	require.NoError(t, db.Model(&profile).Update("model", ImageStudioEditModel).Error)
	profile.Model = ImageStudioEditModel
	specification := imageSubmissionSpec()
	specification.MaxReferenceImages = 2
	encodedSpecification, err := common.Marshal(specification)
	require.NoError(t, err)
	require.NoError(t, db.Model(&profile).Update("specification", string(encodedSpecification)).Error)
	references := []ImageStudioReferenceMetadata{
		{SHA256: strings.Repeat("a", 64), SizeBytes: 100},
		{SHA256: strings.Repeat("b", 64), SizeBytes: 200},
	}
	quoted, err := NormalizeImageStudioSubmission(context.Background(), db, 7, ImageStudioSubmissionRequest{
		TokenID: 4, Model: profile.Model, Prompt: "edit",
		Mode: ImageStudioModeEdit, References: references,
	})
	require.NoError(t, err)
	now := time.Unix(1_800_000_000, 0)
	quote, err := newImageStudioQuoteAt(
		quoted, 300, map[string]float64{"n": 1}, imageStudioQuoteTestSnapshot(quoted), now,
	)
	require.NoError(t, err)

	uploaded, err := NormalizeImageStudioSubmission(context.Background(), db, 7, ImageStudioSubmissionRequest{
		TokenID: 4, Model: profile.Model, Prompt: "edit", QuoteToken: quote.QuoteToken,
		Mode:       ImageStudioModeEdit,
		References: []ImageStudioReferenceMetadata{references[1], references[0]},
	})
	require.NoError(t, err)
	_, err = ValidateImageStudioQuote(uploaded, now.Add(time.Minute))
	assert.ErrorIs(t, err, ErrImageStudioQuoteMismatch)
}

func TestImageStudioQuoteBindsNormalizedCreativeRequest(t *testing.T) {
	db, profile := newImageSubmissionTestDB(t)
	normalized, err := NormalizeImageStudioSubmission(context.Background(), db, 7, ImageStudioSubmissionRequest{
		TokenID: 4, Model: profile.Model, Prompt: "a lighthouse",
	})
	require.NoError(t, err)
	now := time.Unix(1_800_000_000, 0)
	snapshot := imageStudioQuoteTestSnapshot(normalized)
	quote, err := newImageStudioQuoteAt(normalized, 300, map[string]float64{"n": 1}, snapshot, now)
	require.NoError(t, err)

	normalized.QuoteToken = quote.QuoteToken
	claims, err := ValidateImageStudioQuote(normalized, now.Add(time.Minute))
	require.NoError(t, err)
	assert.Equal(t, 300, claims.Quota)
	assert.Equal(t, map[string]float64{"n": 1}, claims.OtherRatios)
	assert.Equal(t, snapshot, claims.ImagePricingSnapshot)

	normalized.Prompt = "different"
	changed, err := imageStudioRequestHash(normalized, imageSubmissionSpec())
	require.NoError(t, err)
	normalized.RequestHash = changed
	_, err = ValidateImageStudioQuote(normalized, now.Add(time.Minute))
	require.ErrorIs(t, err, ErrImageStudioQuoteMismatch)
}

func TestImageStudioBatchQuoteBindsRequestedCountRatio(t *testing.T) {
	db, profile := newImageSubmissionTestDB(t)
	normalized, err := NormalizeImageStudioSubmission(context.Background(), db, 7, ImageStudioSubmissionRequest{
		TokenID: 4, Model: profile.Model, Prompt: "four candidates",
		Parameters: map[string]any{"count": 4},
	})
	require.NoError(t, err)
	now := time.Unix(1_800_000_000, 0)

	_, err = newImageStudioQuoteAt(normalized, 1_000, nil, nil, now)
	assert.ErrorIs(t, err, ErrImageStudioQuoteMismatch)
	_, err = newImageStudioQuoteAt(normalized, 1_000, map[string]float64{"n": 3}, nil, now)
	assert.ErrorIs(t, err, ErrImageStudioQuoteMismatch)

	quote, err := newImageStudioQuoteAt(
		normalized, 1_000, map[string]float64{"n": 4}, nil, now,
	)
	require.NoError(t, err)
	normalized.QuoteToken = quote.QuoteToken
	claims, err := ValidateImageStudioQuote(normalized, now.Add(time.Minute))
	require.NoError(t, err)
	assert.Equal(t, float64(4), claims.OtherRatios["n"])
}

func TestImageStudioQuoteRejectsTamperedSignedClaims(t *testing.T) {
	db, profile := newImageSubmissionTestDB(t)
	normalized, err := NormalizeImageStudioSubmission(context.Background(), db, 7, ImageStudioSubmissionRequest{
		TokenID: 4, Model: profile.Model, Prompt: "a lighthouse",
	})
	require.NoError(t, err)
	now := time.Unix(1_800_000_000, 0)
	quote, err := newImageStudioQuoteAt(
		normalized, 300, map[string]float64{"n": 1}, imageStudioQuoteTestSnapshot(normalized), now,
	)
	require.NoError(t, err)

	tests := []struct {
		name   string
		mutate func(*ImageStudioQuoteClaims)
	}{
		{name: "quota", mutate: func(claims *ImageStudioQuoteClaims) { claims.Quota++ }},
		{name: "unit price", mutate: func(claims *ImageStudioQuoteClaims) { claims.ImagePricingSnapshot.UnitPrice++ }},
		{name: "size", mutate: func(claims *ImageStudioQuoteClaims) { claims.ImagePricingSnapshot.Size = "2048x2048" }},
		{name: "user", mutate: func(claims *ImageStudioQuoteClaims) { claims.UserID++ }},
		{name: "request hash", mutate: func(claims *ImageStudioQuoteClaims) { claims.RequestHash = strings.Repeat("b", 64) }},
		{name: "expiry", mutate: func(claims *ImageStudioQuoteClaims) { claims.ExpiresAt++ }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			normalized.QuoteToken = tamperImageStudioQuoteToken(t, quote.QuoteToken, test.mutate)
			_, validateErr := ValidateImageStudioQuote(normalized, now.Add(time.Minute))
			assert.ErrorIs(t, validateErr, ErrImageStudioQuoteMismatch)
		})
	}
}

func TestImageStudioQuoteRejectsExpiredAndOversizedTokens(t *testing.T) {
	db, profile := newImageSubmissionTestDB(t)
	normalized, err := NormalizeImageStudioSubmission(context.Background(), db, 7, ImageStudioSubmissionRequest{
		TokenID: 4, Model: profile.Model, Prompt: "a lighthouse",
	})
	require.NoError(t, err)
	now := time.Unix(1_800_000_000, 0)
	quote, err := newImageStudioQuoteAt(
		normalized, 300, map[string]float64{"n": 1}, imageStudioQuoteTestSnapshot(normalized), now,
	)
	require.NoError(t, err)

	normalized.QuoteToken = quote.QuoteToken
	_, err = ValidateImageStudioQuote(normalized, now.Add(imageStudioQuoteTTL))
	assert.ErrorIs(t, err, ErrImageStudioQuoteExpired)

	normalized.QuoteToken = strings.Repeat("a", imageStudioQuoteTokenMaxLength+1)
	_, err = ValidateImageStudioQuote(normalized, now)
	assert.ErrorIs(t, err, ErrImageStudioQuoteMismatch)
}

func imageStudioQuoteTestSnapshot(normalized *NormalizedImageStudioSubmission) *imagepricing.Snapshot {
	return &imagepricing.Snapshot{
		PolicyVersion: "test-v1", PolicyHash: strings.Repeat("a", 64),
		Model: normalized.Model, Size: normalized.RelayRequest.Size, Tier: "1k",
		UnitPrice: 1.5, QuotaPerUnit: 100, GroupRatio: 2,
		RequestedCount: normalized.RequestedCount,
	}
}

func tamperImageStudioQuoteToken(
	t *testing.T,
	token string,
	mutate func(*ImageStudioQuoteClaims),
) string {
	t.Helper()
	encodedPayload, signature, found := strings.Cut(token, ".")
	require.True(t, found)
	payload, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	require.NoError(t, err)
	var claims ImageStudioQuoteClaims
	require.NoError(t, common.Unmarshal(payload, &claims))
	mutate(&claims)
	payload, err = common.Marshal(claims)
	require.NoError(t, err)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + signature
}

func TestCreateImageGenerationAtomicallyBindsIdempotencyReservation(t *testing.T) {
	db, profile := newImageSubmissionTestDB(t)
	normalized, err := NormalizeImageStudioSubmission(context.Background(), db, 7, ImageStudioSubmissionRequest{
		TokenID: 4, Model: profile.Model, Prompt: "a lighthouse",
	})
	require.NoError(t, err)
	reservation, err := ReserveIdempotencyKey(context.Background(), db, IdempotencyReservationRequest{
		UserID: 7, Operation: model.ImageIdempotencyOperationSubmit,
		Key: "image-submit-key", RequestHash: normalized.RequestHash,
	})
	require.NoError(t, err)

	generation, err := CreateImageGeneration(
		context.Background(), db, normalized, "req-image-generation", reservation.Record.ID,
	)
	require.NoError(t, err)
	assert.Equal(t, model.ImageGenerationStatusSubmitting, generation.Status)

	var bound model.KKAIIdempotencyKey
	require.NoError(t, db.First(&bound, reservation.Record.ID).Error)
	assert.Equal(t, model.ImageIdempotencyResourceGeneration, bound.ResourceType)
	assert.Equal(t, "1", bound.ResourceID)
}

func TestReconcileStaleImageGenerationsNeverRetriesSupplier(t *testing.T) {
	db, profile := newImageSubmissionTestDB(t)
	old := time.Now().Add(-10 * time.Minute).Unix()
	generation := model.KKAIImageGeneration{
		UserID: 7, TokenID: 4, ModelProfileID: profile.ID, SpecificationVersion: 1,
		Model: profile.Model, Prompt: "prompt", Parameters: "{}",
		RequestHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RequestID:   "req-stale-image", Status: model.ImageGenerationStatusSubmitting,
		RequestedCount: 1, StartedAt: old, CreatedAt: old, UpdatedAt: old,
	}
	require.NoError(t, db.Create(&generation).Error)

	updated, err := ReconcileStaleImageGenerations(context.Background(), db, time.Now().Add(-time.Minute), 10)
	require.NoError(t, err)
	assert.Equal(t, 1, updated)
	require.NoError(t, db.First(&generation, generation.ID).Error)
	assert.Equal(t, model.ImageGenerationStatusUnknown, generation.Status)
	assert.Equal(t, "submission_interrupted", generation.ErrorCode)
}

func TestReconcileStaleImageGenerationsIgnoresDeletedGeneration(t *testing.T) {
	db, profile := newImageSubmissionTestDB(t)
	old := time.Now().Add(-10 * time.Minute).Unix()
	generation := model.KKAIImageGeneration{
		UserID: 7, TokenID: 4, ModelProfileID: profile.ID, SpecificationVersion: 1,
		Model: profile.Model, Prompt: "deleted prompt", Parameters: "{}",
		RequestHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		RequestID:   "req-stale-deleted-image", Status: model.ImageGenerationStatusSubmitting,
		RequestedCount: 1, HeartbeatAt: old, StartedAt: old, CreatedAt: old, UpdatedAt: old,
		DeletedAt: time.Now().Unix(),
	}
	require.NoError(t, db.Create(&generation).Error)

	updated, err := ReconcileStaleImageGenerations(
		context.Background(), db, time.Now().Add(-time.Minute), 10,
	)
	require.NoError(t, err)
	assert.Zero(t, updated)
	claimed, err := reconcileStaleImageGeneration(
		context.Background(), db, generation.ID, time.Now().Add(-time.Minute),
	)
	require.NoError(t, err)
	assert.False(t, claimed)
	require.NoError(t, db.First(&generation, generation.ID).Error)
	assert.Equal(t, model.ImageGenerationStatusSubmitting, generation.Status)
	assert.NotZero(t, generation.DeletedAt)
}

func TestFinishRecoveringImageGenerationCASRejectsConcurrentDeletion(t *testing.T) {
	db, profile := newImageSubmissionTestDB(t)
	now := time.Now().Unix()
	generation := model.KKAIImageGeneration{
		UserID: 7, TokenID: 4, ModelProfileID: profile.ID, SpecificationVersion: 1,
		Model: profile.Model, Prompt: "recovering delete race", Parameters: "{}",
		RequestHash: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		RequestID:   "req-recovering-delete-race", Status: model.ImageGenerationStatusRecovering,
		RequestedCount: 1, BillingState: model.ImageGenerationBillingStateRefunded,
		HeartbeatAt: now, StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&generation).Error)
	injected := false
	callbackName := "test:image_generation_recovery_delete_race"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if injected || tx.Statement.Table != (model.KKAIImageGeneration{}).TableName() {
			return
		}
		injected = true
		if err := tx.Exec(
			"UPDATE kkai_image_generations SET deleted_at = ? WHERE id = ?",
			time.Now().Unix(), generation.ID,
		).Error; err != nil {
			tx.AddError(err)
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Update().Remove(callbackName) })

	err := finishRecoveringImageGeneration(
		context.Background(), db, generation.ID, model.ImageGenerationStatusUnknown,
		0, 0, "reconcile", "submission_interrupted", "submission outcome is unknown",
	)
	require.ErrorIs(t, err, ErrImageGenerationConflict)
	require.True(t, injected)
	require.NoError(t, db.First(&generation, generation.ID).Error)
	assert.Equal(t, model.ImageGenerationStatusRecovering, generation.Status)
	assert.Zero(t, generation.FinishedAt)
	assert.Zero(t, generation.DeletedAt)
}

func TestReconcileStaleImageGenerationSettlesPreparedQuotaExactlyOnce(t *testing.T) {
	db := newImageAccountingRecoveryTestDB(t)
	require.NoError(t, db.Create(&model.User{
		Id: 71, Username: "image-recovery", Password: "password", Quota: 1_000,
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id: 3, Name: "image-recovery-channel", Key: "test-key", Status: 1, Type: 1,
	}).Error)
	generation := seedRecoverableImageGeneration(t, db, 71, 2)
	_, err := model.ReserveImageGenerationBilling(
		context.Background(), db, generation.ID, model.TaskBillingSourceWallet, 500,
	)
	require.NoError(t, err)
	require.NoError(t, model.MarkImageGenerationDispatching(context.Background(), db, generation.ID))
	require.NoError(t, model.PrepareImageGenerationAccounting(
		context.Background(), db, generation.ID, model.ImageGenerationAccountingPayload{
			TargetQuota: 200, CountStatistics: true, OutputCount: 2,
			LogParams: model.RecordConsumeLogParams{
				ChannelId: 3, ModelName: generation.Model, TokenId: generation.TokenID, Quota: 200,
			},
		},
	))
	old := time.Now().Add(-10 * time.Minute).Unix()
	for position := 0; position < 2; position++ {
		require.NoError(t, db.Create(&model.KKAIImageAsset{
			GenerationID: &generation.ID, OwnerUserID: 71, Scope: model.ImageAssetScopeUser,
			Kind: model.ImageAssetKindOutput, State: model.ImageAssetStateReady, Position: position,
			ObjectKey:      fmt.Sprintf("image-recovery/%d", position),
			ThumbnailState: model.ImageThumbnailStatePending, CreatedAt: old, UpdatedAt: old,
		}).Error)
	}
	require.NoError(t, db.Model(&model.KKAIImageGeneration{}).Where("id = ?", generation.ID).
		Update("heartbeat_at", old).Error)

	updated, err := ReconcileStaleImageGenerations(
		context.Background(), db, time.Now().Add(-time.Minute), 10,
	)
	require.NoError(t, err)
	require.Equal(t, 1, updated)
	updated, err = ReconcileStaleImageGenerations(
		context.Background(), db, time.Now().Add(-time.Minute), 10,
	)
	require.NoError(t, err)
	require.Zero(t, updated)
	require.NoError(t, db.First(&generation, generation.ID).Error)
	require.Equal(t, model.ImageGenerationStatusSucceeded, generation.Status)
	require.Equal(t, model.ImageGenerationBillingStateSettled, generation.BillingState)
	require.Equal(t, 200, generation.FinalQuota)
	var user model.User
	require.NoError(t, db.First(&user, 71).Error)
	require.EqualValues(t, 800, user.Quota)
	require.EqualValues(t, 200, user.UsedQuota)
	require.EqualValues(t, 1, user.RequestCount)
}

func TestReconcileStaleImageGenerationSettlesPartialDelivery(t *testing.T) {
	db := newImageAccountingRecoveryTestDB(t)
	require.NoError(t, db.Create(&model.User{
		Id: 72, Username: "image-partial", Password: "password", Quota: 1_000,
	}).Error)
	generation := seedRecoverableImageGeneration(t, db, 72, 2)
	_, err := model.ReserveImageGenerationBilling(
		context.Background(), db, generation.ID, model.TaskBillingSourceWallet, 500,
	)
	require.NoError(t, err)
	require.NoError(t, model.MarkImageGenerationDispatching(context.Background(), db, generation.ID))
	require.NoError(t, model.PrepareImageGenerationAccounting(
		context.Background(), db, generation.ID, model.ImageGenerationAccountingPayload{
			TargetQuota: 200, OutputCount: 1,
			LogParams: model.RecordConsumeLogParams{
				ChannelId: 3, ModelName: generation.Model, TokenId: generation.TokenID, Quota: 200,
			},
		},
	))
	old := time.Now().Add(-10 * time.Minute).Unix()
	asset := model.KKAIImageAsset{
		GenerationID: &generation.ID, OwnerUserID: 72, Scope: model.ImageAssetScopeUser,
		Kind: model.ImageAssetKindOutput, State: model.ImageAssetStateReady,
		ObjectKey: "image-partial/ready", ThumbnailState: model.ImageThumbnailStatePending,
		CreatedAt: old, UpdatedAt: old,
	}
	require.NoError(t, db.Create(&asset).Error)
	require.NoError(t, db.Model(&model.KKAIImageGeneration{}).Where("id = ?", generation.ID).
		Update("heartbeat_at", old).Error)

	updated, err := ReconcileStaleImageGenerations(
		context.Background(), db, time.Now().Add(-time.Minute), 10,
	)
	require.NoError(t, err)
	require.Equal(t, 1, updated)
	require.NoError(t, db.First(&generation, generation.ID).Error)
	require.Equal(t, model.ImageGenerationStatusPartial, generation.Status)
	require.Equal(t, model.ImageGenerationBillingStateSettled, generation.BillingState)
	require.Equal(t, 1, generation.SucceededCount)
	require.Equal(t, 200, generation.FinalQuota)
	require.NoError(t, db.First(&asset, asset.ID).Error)
	require.Equal(t, model.ImageAssetStateReady, asset.State)
	require.Zero(t, asset.DeletedAt)
	var user model.User
	require.NoError(t, db.First(&user, 72).Error)
	require.EqualValues(t, 800, user.Quota)
	var deletionEvents int64
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where(
		"topic = ?", ImageAssetDeleteTopic,
	).Count(&deletionEvents).Error)
	require.Zero(t, deletionEvents)
}

func TestReconcileStaleImageGenerationRefundsLegacyPartialAccountingMismatch(t *testing.T) {
	db := newImageAccountingRecoveryTestDB(t)
	require.NoError(t, db.Create(&model.User{
		Id: 73, Username: "image-legacy-partial", Password: "password", Quota: 1_000,
	}).Error)
	generation := seedRecoverableImageGeneration(t, db, 73, 2)
	_, err := model.ReserveImageGenerationBilling(
		context.Background(), db, generation.ID, model.TaskBillingSourceWallet, 500,
	)
	require.NoError(t, err)
	require.NoError(t, model.MarkImageGenerationDispatching(context.Background(), db, generation.ID))
	old := time.Now().Add(-10 * time.Minute).Unix()
	legacyAccounting, err := common.Marshal(model.ImageGenerationAccountingPayload{
		GenerationID: generation.ID, TargetQuota: 200,
		LogParams: model.RecordConsumeLogParams{
			ChannelId: 3, ModelName: generation.Model, TokenId: generation.TokenID, Quota: 200,
		},
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.KKAIOutboxEvent{
		EventKey: fmt.Sprintf("image-accounting:%d", generation.ID),
		Topic:    model.KKAIOutboxTopicImageAccounting, AggregateID: fmt.Sprintf("%d", generation.ID),
		Payload: string(legacyAccounting), Status: model.KKAIOutboxStatusPending,
		AvailableAt: old, CreatedAt: old,
	}).Error)
	asset := model.KKAIImageAsset{
		GenerationID: &generation.ID, OwnerUserID: 73, Scope: model.ImageAssetScopeUser,
		Kind: model.ImageAssetKindOutput, State: model.ImageAssetStateReady,
		ObjectKey: "image-legacy-partial/ready", ThumbnailState: model.ImageThumbnailStatePending,
		CreatedAt: old, UpdatedAt: old,
	}
	require.NoError(t, db.Create(&asset).Error)
	require.NoError(t, db.Model(&model.KKAIImageGeneration{}).Where("id = ?", generation.ID).
		Update("heartbeat_at", old).Error)

	updated, err := ReconcileStaleImageGenerations(
		context.Background(), db, time.Now().Add(-time.Minute), 10,
	)
	require.NoError(t, err)
	require.Equal(t, 1, updated)
	require.NoError(t, db.First(&generation, generation.ID).Error)
	require.Equal(t, model.ImageGenerationStatusArchiveFailed, generation.Status)
	require.Equal(t, model.ImageGenerationBillingStateRefunded, generation.BillingState)
	require.Zero(t, generation.SucceededCount)
	require.Zero(t, generation.FinalQuota)
	require.NoError(t, db.Unscoped().First(&asset, asset.ID).Error)
	require.Equal(t, model.ImageAssetStateDeleted, asset.State)
	require.NotZero(t, asset.DeletedAt)
	var user model.User
	require.NoError(t, db.First(&user, 73).Error)
	require.EqualValues(t, 1_000, user.Quota)
	require.Zero(t, user.UsedQuota)
}

func TestReconcileStaleImageGenerationsRefundsCountMismatchAndContinues(t *testing.T) {
	db := newImageAccountingRecoveryTestDB(t)
	require.NoError(t, db.Create(&model.User{
		Id: 74, Username: "image-count-mismatch", Password: "password", Quota: 1_000,
	}).Error)
	require.NoError(t, db.Create(&model.User{
		Id: 75, Username: "image-following-recovery", Password: "password", Quota: 1_000,
		AffCode: "following-recovery",
	}).Error)
	mismatched := seedRecoverableImageGeneration(t, db, 74, 2)
	_, err := model.ReserveImageGenerationBilling(
		context.Background(), db, mismatched.ID, model.TaskBillingSourceWallet, 500,
	)
	require.NoError(t, err)
	require.NoError(t, model.MarkImageGenerationDispatching(context.Background(), db, mismatched.ID))
	require.NoError(t, model.PrepareImageGenerationAccounting(
		context.Background(), db, mismatched.ID, model.ImageGenerationAccountingPayload{
			TargetQuota: 200, OutputCount: 2,
			LogParams: model.RecordConsumeLogParams{
				ChannelId: 3, ModelName: mismatched.Model, TokenId: mismatched.TokenID, Quota: 200,
			},
		},
	))
	old := time.Now().Add(-10 * time.Minute).Unix()
	mismatchedAsset := model.KKAIImageAsset{
		GenerationID: &mismatched.ID, OwnerUserID: 74, Scope: model.ImageAssetScopeUser,
		Kind: model.ImageAssetKindOutput, State: model.ImageAssetStateReady,
		ObjectKey: "image-count-mismatch/ready", ThumbnailState: model.ImageThumbnailStatePending,
		CreatedAt: old, UpdatedAt: old,
	}
	require.NoError(t, db.Create(&mismatchedAsset).Error)
	require.NoError(t, db.Model(&model.KKAIImageGeneration{}).Where("id = ?", mismatched.ID).
		Update("heartbeat_at", old).Error)

	following := seedRecoverableImageGeneration(t, db, 75, 1)
	require.Greater(t, following.ID, mismatched.ID)
	_, err = model.ReserveImageGenerationBilling(
		context.Background(), db, following.ID, model.TaskBillingSourceWallet, 300,
	)
	require.NoError(t, err)
	require.NoError(t, model.MarkImageGenerationDispatching(context.Background(), db, following.ID))
	require.NoError(t, model.PrepareImageGenerationAccounting(
		context.Background(), db, following.ID, model.ImageGenerationAccountingPayload{
			TargetQuota: 100, OutputCount: 1,
			LogParams: model.RecordConsumeLogParams{
				ChannelId: 3, ModelName: following.Model, TokenId: following.TokenID, Quota: 100,
			},
		},
	))
	followingAsset := model.KKAIImageAsset{
		GenerationID: &following.ID, OwnerUserID: 75, Scope: model.ImageAssetScopeUser,
		Kind: model.ImageAssetKindOutput, State: model.ImageAssetStateReady,
		ObjectKey: "image-following-recovery/ready", ThumbnailState: model.ImageThumbnailStatePending,
		CreatedAt: old, UpdatedAt: old,
	}
	require.NoError(t, db.Create(&followingAsset).Error)
	require.NoError(t, db.Model(&model.KKAIImageGeneration{}).Where("id = ?", following.ID).
		Update("heartbeat_at", old).Error)

	updated, err := ReconcileStaleImageGenerations(
		context.Background(), db, time.Now().Add(-time.Minute), 10,
	)
	require.NoError(t, err)
	require.Equal(t, 2, updated)

	require.NoError(t, db.First(&mismatched, mismatched.ID).Error)
	assert.Equal(t, model.ImageGenerationStatusArchiveFailed, mismatched.Status)
	assert.Equal(t, model.ImageGenerationBillingStateRefunded, mismatched.BillingState)
	assert.Zero(t, mismatched.SucceededCount)
	assert.Zero(t, mismatched.FinalQuota)
	require.NoError(t, db.Unscoped().First(&mismatchedAsset, mismatchedAsset.ID).Error)
	assert.Equal(t, model.ImageAssetStateDeleted, mismatchedAsset.State)
	assert.NotZero(t, mismatchedAsset.DeletedAt)
	var mismatchedUser model.User
	require.NoError(t, db.First(&mismatchedUser, 74).Error)
	assert.EqualValues(t, 1_000, mismatchedUser.Quota)

	require.NoError(t, db.First(&following, following.ID).Error)
	assert.Equal(t, model.ImageGenerationStatusSucceeded, following.Status)
	assert.Equal(t, model.ImageGenerationBillingStateSettled, following.BillingState)
	assert.Equal(t, 1, following.SucceededCount)
	assert.Equal(t, 100, following.FinalQuota)
	require.NoError(t, db.First(&followingAsset, followingAsset.ID).Error)
	assert.Equal(t, model.ImageAssetStateReady, followingAsset.State)
	assert.Zero(t, followingAsset.DeletedAt)
	var followingUser model.User
	require.NoError(t, db.First(&followingUser, 75).Error)
	assert.EqualValues(t, 900, followingUser.Quota)
}

func TestReconcileStaleImageGenerationsPreservesSettledAssetsAndContinues(t *testing.T) {
	db := newImageAccountingRecoveryTestDB(t)
	require.NoError(t, db.Create(&model.User{
		Id: 76, Username: "image-settled-mismatch", Password: "password", Quota: 1_000,
		AffCode: "settled-mismatch",
	}).Error)
	require.NoError(t, db.Create(&model.User{
		Id: 77, Username: "image-after-settled", Password: "password", Quota: 1_000,
		AffCode: "after-settled",
	}).Error)

	settled := seedRecoverableImageGeneration(t, db, 76, 2)
	_, err := model.ReserveImageGenerationBilling(
		context.Background(), db, settled.ID, model.TaskBillingSourceWallet, 500,
	)
	require.NoError(t, err)
	require.NoError(t, model.MarkImageGenerationDispatching(context.Background(), db, settled.ID))
	require.NoError(t, model.PrepareImageGenerationAccounting(
		context.Background(), db, settled.ID, model.ImageGenerationAccountingPayload{
			TargetQuota: 200, OutputCount: 2,
			LogParams: model.RecordConsumeLogParams{
				ChannelId: 3, ModelName: settled.Model, TokenId: settled.TokenID, Quota: 200,
			},
		},
	))
	_, err = model.SettleImageGenerationBilling(context.Background(), db, settled.ID, 200)
	require.NoError(t, err)
	old := time.Now().Add(-10 * time.Minute).Unix()
	settledReadyAsset := model.KKAIImageAsset{
		GenerationID: &settled.ID, OwnerUserID: 76, Scope: model.ImageAssetScopeUser,
		Kind: model.ImageAssetKindOutput, State: model.ImageAssetStateReady, Position: 0,
		ObjectKey: "image-settled-mismatch/ready", ThumbnailState: model.ImageThumbnailStatePending,
		CreatedAt: old, UpdatedAt: old,
	}
	require.NoError(t, db.Create(&settledReadyAsset).Error)
	settledStagingAsset := model.KKAIImageAsset{
		GenerationID: &settled.ID, OwnerUserID: 76, Scope: model.ImageAssetScopeUser,
		Kind: model.ImageAssetKindOutput, State: model.ImageAssetStateStaging, Position: 1,
		ObjectKey: "image-settled-mismatch/staging", ThumbnailState: model.ImageThumbnailStatePending,
		CreatedAt: old, UpdatedAt: old,
	}
	require.NoError(t, db.Create(&settledStagingAsset).Error)
	require.NoError(t, db.Model(&model.KKAIImageGeneration{}).Where("id = ?", settled.ID).
		Update("heartbeat_at", old).Error)

	following := seedRecoverableImageGeneration(t, db, 77, 1)
	require.Greater(t, following.ID, settled.ID)
	_, err = model.ReserveImageGenerationBilling(
		context.Background(), db, following.ID, model.TaskBillingSourceWallet, 300,
	)
	require.NoError(t, err)
	require.NoError(t, model.MarkImageGenerationDispatching(context.Background(), db, following.ID))
	require.NoError(t, model.PrepareImageGenerationAccounting(
		context.Background(), db, following.ID, model.ImageGenerationAccountingPayload{
			TargetQuota: 100, OutputCount: 1,
			LogParams: model.RecordConsumeLogParams{
				ChannelId: 3, ModelName: following.Model, TokenId: following.TokenID, Quota: 100,
			},
		},
	))
	followingAsset := model.KKAIImageAsset{
		GenerationID: &following.ID, OwnerUserID: 77, Scope: model.ImageAssetScopeUser,
		Kind: model.ImageAssetKindOutput, State: model.ImageAssetStateReady,
		ObjectKey: "image-after-settled/ready", ThumbnailState: model.ImageThumbnailStatePending,
		CreatedAt: old, UpdatedAt: old,
	}
	require.NoError(t, db.Create(&followingAsset).Error)
	require.NoError(t, db.Model(&model.KKAIImageGeneration{}).Where("id = ?", following.ID).
		Update("heartbeat_at", old).Error)

	updated, err := ReconcileStaleImageGenerations(
		context.Background(), db, time.Now().Add(-time.Minute), 10,
	)
	require.ErrorIs(t, err, ErrImageGenerationConflict)
	assert.Equal(t, 1, updated)

	require.NoError(t, db.First(&settled, settled.ID).Error)
	assert.Equal(t, model.ImageGenerationStatusRecovering, settled.Status)
	assert.Equal(t, model.ImageGenerationBillingStateSettled, settled.BillingState)
	assert.Equal(t, 200, settled.FinalQuota)
	require.NoError(t, db.First(&settledReadyAsset, settledReadyAsset.ID).Error)
	assert.Equal(t, model.ImageAssetStateReady, settledReadyAsset.State)
	assert.Zero(t, settledReadyAsset.DeletedAt)
	require.NoError(t, db.First(&settledStagingAsset, settledStagingAsset.ID).Error)
	assert.Equal(t, model.ImageAssetStateStaging, settledStagingAsset.State)
	assert.Zero(t, settledStagingAsset.DeletedAt)

	require.NoError(t, db.First(&following, following.ID).Error)
	assert.Equal(t, model.ImageGenerationStatusSucceeded, following.Status)
	assert.Equal(t, model.ImageGenerationBillingStateSettled, following.BillingState)
	assert.Equal(t, 1, following.SucceededCount)
	require.NoError(t, db.First(&followingAsset, followingAsset.ID).Error)
	assert.Equal(t, model.ImageAssetStateReady, followingAsset.State)
}

func TestImageStudioBillingGuardRejectsPriceIncreaseWithoutChargingConfirmedMaximum(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	require.NoError(t, SetImageStudioBillingGuard(c, 100))
	require.NoError(t, EnforceImageStudioPreconsumeLimit(c, 100))
	require.ErrorIs(t, EnforceImageStudioPreconsumeLimit(c, 101), ErrImageStudioQuoteStale)

	quota, capped := applyImageStudioFinalQuota(c, 120)
	assert.Equal(t, 0, quota)
	assert.True(t, capped)
	finalQuota, settled, recordedCapped := ImageStudioFinalQuota(c)
	assert.Equal(t, 0, finalQuota)
	assert.False(t, settled)
	assert.True(t, recordedCapped)
	require.True(t, completeImageStudioBilling(c, nil))
	finalQuota, settled, recordedCapped = ImageStudioFinalQuota(c)
	assert.Equal(t, 0, finalQuota)
	assert.True(t, settled)
	assert.True(t, recordedCapped)
}

func newImageSubmissionTestDB(t *testing.T) (*gorm.DB, model.KKAIImageModelProfile) {
	t.Helper()
	dsn := "file:image-submission-" + time.Now().Format("150405.000000000") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.KKAIImageModelProfile{}, &model.KKAIImageSample{}, &model.KKAIImageGeneration{},
		&model.KKAIImageAsset{}, &model.KKAIIdempotencyKey{}, &model.KKAIOutboxEvent{},
	))
	specification, err := common.Marshal(imageSubmissionSpec())
	require.NoError(t, err)
	defaults, err := common.Marshal(map[string]any{"count": 1, "size": "1024x1024"})
	require.NoError(t, err)
	now := time.Now().Unix()
	profile := model.KKAIImageModelProfile{
		Model: "image-model-v1", DisplayName: "Image model", SpecificationVersion: 1,
		Specification: string(specification), DefaultParameters: string(defaults), Enabled: true,
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&profile).Error)
	return db, profile
}

func newImageAccountingRecoveryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:image-accounting-recovery-" + time.Now().Format("150405.000000000") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.Channel{}, &model.KKAIImageGeneration{},
		&model.KKAIImageAsset{}, &model.KKAIOutboxEvent{},
	))
	return db
}

func seedRecoverableImageGeneration(
	t *testing.T, db *gorm.DB, userID int, requestedCount int,
) model.KKAIImageGeneration {
	t.Helper()
	now := time.Now().Unix()
	generation := model.KKAIImageGeneration{
		UserID: userID, TokenID: 4, ModelProfileID: 1, SpecificationVersion: 1,
		Model: "gpt-image-1", Prompt: "prompt", Parameters: "{}",
		RequestHash: fmt.Sprintf("%064d", time.Now().UnixNano()),
		RequestID:   fmt.Sprintf("req-image-recovery-%d", time.Now().UnixNano()),
		Status:      model.ImageGenerationStatusSubmitting, RequestedCount: requestedCount,
		BillingState: model.ImageGenerationBillingStatePending,
		HeartbeatAt:  now, StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&generation).Error)
	return generation
}

func imageSubmissionSpec() ImageModelSpec {
	minimum := 1
	maximum := 4
	return ImageModelSpec{Version: 1, Parameters: []ImageParameterSpec{
		{Key: "count", Label: "Count", Control: ImageControlInteger, RequestKey: "n", Min: &minimum, Max: &maximum},
		{Key: "size", Label: "Size", Control: ImageControlSelect, RequestKey: "size", Options: []ImageParameterOption{
			{Label: "Square", Value: "1024x1024"},
		}},
	}}
}
