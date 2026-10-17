package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageAsyncSchemaRequiresVerifiedMigrationBeyondSidecarTables(t *testing.T) {
	db := newImageAccountingRecoveryTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.KKAIImageExecution{}, &model.KKAIImageReferenceUpload{}, &model.KKAIImageGenerationReference{}))
	assert.False(t, ImageAsyncSchemaReady(context.Background(), db))
}
