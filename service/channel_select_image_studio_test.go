package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestImageStudioChannelSelectionFiltersEveryPriority(t *testing.T) {
	db := newImageStudioChannelSelectionTestDB(t)
	const (
		groupName = "image-selection"
		modelName = "gpt-image-2"
	)
	replicate := seedImageStudioSelectionChannel(t, db, groupName, modelName, constant.ChannelTypeReplicate, 30)
	openAI := seedImageStudioSelectionChannel(t, db, groupName, modelName, constant.ChannelTypeOpenAI, 20)
	ali := seedImageStudioSelectionChannel(t, db, groupName, modelName, constant.ChannelTypeAli, 10)

	for _, memoryCacheEnabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("memory_cache_%t", memoryCacheEnabled), func(t *testing.T) {
			common.MemoryCacheEnabled = memoryCacheEnabled
			if memoryCacheEnabled {
				require.NoError(t, model.SyncChannelCacheOnce())
			}

			multiReferenceContext := imageStudioChannelSelectionContextWithOutputs(2, 2)
			selected, _, err := CacheGetRandomSatisfiedChannel(&RetryParam{
				Ctx: multiReferenceContext, TokenGroup: groupName, ModelName: modelName,
				RequestPath: "/v1/images/edits", Retry: common.GetPointer(0),
			})
			require.NoError(t, err)
			require.NotNil(t, selected)
			assert.Equal(t, openAI.Id, selected.Id)

			selected, _, err = CacheGetRandomSatisfiedChannel(&RetryParam{
				Ctx: multiReferenceContext, TokenGroup: groupName, ModelName: modelName,
				RequestPath: "/v1/images/edits", Retry: common.GetPointer(1),
			})
			require.NoError(t, err)
			require.NotNil(t, selected)
			assert.Equal(t, ali.Id, selected.Id)

			singleReferenceContext := imageStudioChannelSelectionContext(1)
			selected, _, err = CacheGetRandomSatisfiedChannel(&RetryParam{
				Ctx: singleReferenceContext, TokenGroup: groupName, ModelName: modelName,
				RequestPath: "/v1/images/edits", Retry: common.GetPointer(0),
			})
			require.NoError(t, err)
			require.NotNil(t, selected)
			assert.Equal(t, replicate.Id, selected.Id)
		})
	}
}

func TestImageStudioChannelOutputCapabilities(t *testing.T) {
	tests := []struct {
		name        string
		channelType int
		want        int
	}{
		{"openai", constant.ChannelTypeOpenAI, MaxImageStudioOutputs},
		{"gemini", constant.ChannelTypeGemini, MaxImageStudioOutputs},
		{"vertex", constant.ChannelTypeVertexAi, MaxImageStudioOutputs},
		{"minimax", constant.ChannelTypeMiniMax, MaxImageStudioOutputs},
		{"replicate", constant.ChannelTypeReplicate, MaxImageStudioOutputs},
		{"ali", constant.ChannelTypeAli, MaxImageStudioOutputs},
		{"azure", constant.ChannelTypeAzure, 1},
		{"unknown", constant.ChannelTypeUnknown, 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, ImageStudioChannelMaxOutputs(test.channelType))
		})
	}
}

func TestImageStudioChannelSelectionFiltersUnsupportedOutputCountsAcrossPriorities(t *testing.T) {
	db := newImageStudioChannelSelectionTestDB(t)
	const (
		groupName = "image-output-selection"
		modelName = "gpt-image-2"
	)
	seedImageStudioSelectionChannel(t, db, groupName, modelName, constant.ChannelTypeAzure, 30)
	gemini := seedImageStudioSelectionChannel(t, db, groupName, modelName, constant.ChannelTypeGemini, 20)
	replicate := seedImageStudioSelectionChannel(t, db, groupName, modelName, constant.ChannelTypeReplicate, 10)
	common.MemoryCacheEnabled = true
	require.NoError(t, model.SyncChannelCacheOnce())

	requestContext := imageStudioChannelSelectionContextWithOutputs(0, 2)
	selected, _, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx: requestContext, TokenGroup: groupName, ModelName: modelName,
		RequestPath: "/v1/images/generations", Retry: common.GetPointer(0),
	})
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, gemini.Id, selected.Id)

	selected, _, err = CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx: requestContext, TokenGroup: groupName, ModelName: modelName,
		RequestPath: "/v1/images/generations", Retry: common.GetPointer(1),
	})
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, replicate.Id, selected.Id)
}

func TestImageStudioChannelSelectionReportsUnsupportedOutputCount(t *testing.T) {
	db := newImageStudioChannelSelectionTestDB(t)
	const (
		groupName = "single-output-only"
		modelName = "gpt-image-2"
	)
	seedImageStudioSelectionChannel(t, db, groupName, modelName, constant.ChannelTypeAzure, 10)
	common.MemoryCacheEnabled = true
	require.NoError(t, model.SyncChannelCacheOnce())

	selected, _, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx: imageStudioChannelSelectionContextWithOutputs(0, 2), TokenGroup: groupName, ModelName: modelName,
		RequestPath: "/v1/images/generations", Retry: common.GetPointer(0),
	})
	assert.Nil(t, selected)
	assert.ErrorIs(t, err, ErrNoChannelSupportsImageOutputCount)
}

func TestImageStudioChannelSelectionReportsUnsupportedMultiReferenceRequest(t *testing.T) {
	db := newImageStudioChannelSelectionTestDB(t)
	const (
		groupName = "replicate-only"
		modelName = "gpt-image-2"
	)
	seedImageStudioSelectionChannel(t, db, groupName, modelName, constant.ChannelTypeReplicate, 10)
	common.MemoryCacheEnabled = true
	require.NoError(t, model.SyncChannelCacheOnce())

	selected, _, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx: imageStudioChannelSelectionContext(2), TokenGroup: groupName, ModelName: modelName,
		RequestPath: "/v1/images/edits", Retry: common.GetPointer(0),
	})
	assert.Nil(t, selected)
	assert.ErrorIs(t, err, ErrNoChannelSupportsImageReferences)
}

func imageStudioChannelSelectionContext(referenceCount int) *gin.Context {
	return imageStudioChannelSelectionContextWithOutputs(referenceCount, 1)
}

func imageStudioChannelSelectionContextWithOutputs(referenceCount int, outputCount int) *gin.Context {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", nil)
	SetImageStudioReferenceCount(ctx, referenceCount)
	SetImageStudioRequestedOutputCount(ctx, outputCount)
	return ctx
}

func newImageStudioChannelSelectionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:image-channel-selection-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	previousDB := model.DB
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
	})
	return db
}

func seedImageStudioSelectionChannel(
	t *testing.T,
	db *gorm.DB,
	groupName string,
	modelName string,
	channelType int,
	priority int64,
) model.Channel {
	t.Helper()
	weight := uint(100)
	channel := model.Channel{
		Type: channelType, Key: fmt.Sprintf("key-%d", channelType), Status: common.ChannelStatusEnabled,
		Name: fmt.Sprintf("channel-%d", channelType), Models: modelName, Group: groupName,
		Priority: &priority, Weight: &weight,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: groupName, Model: modelName, ChannelId: channel.Id, Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	return channel
}
