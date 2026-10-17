package service

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageSubmissionLookupIsReadOnlyAndScopedToOwner(t *testing.T) {
	db, _ := newImageSubmissionTestDB(t)
	generation := seedRecoverableImageGeneration(t, db, 7, 1)
	now := time.Now().Unix()
	bindings := []model.KKAIIdempotencyKey{
		{UserID: 7, Operation: model.ImageIdempotencyOperationSubmit, Key: "expired-found", ResourceType: model.ImageIdempotencyResourceGeneration, ResourceID: strconv.FormatInt(generation.ID, 10), ExpiresAt: now - 1},
		{UserID: 7, Operation: model.ImageIdempotencyOperationSubmit, Key: "expired-pending", ExpiresAt: now - 1},
		{UserID: 8, Operation: model.ImageIdempotencyOperationSubmit, Key: "wrong-owner", ResourceType: model.ImageIdempotencyResourceGeneration, ResourceID: strconv.FormatInt(generation.ID, 10), ExpiresAt: now + 100},
		{UserID: 7, Operation: "video.submit", Key: "wrong-operation", ResourceType: model.ImageIdempotencyResourceGeneration, ResourceID: strconv.FormatInt(generation.ID, 10), ExpiresAt: now + 100},
	}
	require.NoError(t, db.Create(&bindings).Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	// SQLite rejects any attempted reservation, expiry cleanup, or task update.
	require.NoError(t, db.Exec("PRAGMA query_only = ON").Error)
	t.Cleanup(func() { require.NoError(t, db.Exec("PRAGMA query_only = OFF").Error) })

	for _, test := range []struct {
		name  string
		user  int
		key   string
		state string
	}{
		{"expired binding remains recoverable", 7, "expired-found", "found"},
		{"expired pending remains inconclusive", 7, "expired-pending", "pending"},
		{"missing key remains inconclusive", 7, "missing", "not_found"},
		{"another user cannot see binding", 8, "expired-found", "not_found"},
		{"binding cannot expose another users task", 8, "wrong-owner", "not_found"},
		{"other operation cannot match", 7, "wrong-operation", "not_found"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := LookupImageSubmission(context.Background(), db, test.user, test.key)
			require.NoError(t, err)
			assert.Equal(t, test.state, result.State)
			if test.state == "found" {
				require.NotNil(t, result.Generation)
				assert.Equal(t, generation.ID, result.Generation.ID)
			} else {
				assert.Nil(t, result.Generation)
			}
		})
	}
	var after []model.KKAIIdempotencyKey
	require.NoError(t, db.Order("id").Find(&after).Error)
	assert.Equal(t, bindings, after)
	var afterGeneration model.KKAIImageGeneration
	require.NoError(t, db.First(&afterGeneration, generation.ID).Error)
	assert.Equal(t, generation, afterGeneration)
	_, err = LookupImageSubmission(context.Background(), db, 7, strings.Repeat("x", 129))
	assert.ErrorIs(t, err, ErrInvalidIdempotencyRequest)
}
