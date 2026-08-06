package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/image_pricing_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedImageStudioAbility(t *testing.T, modelName string) {
	t.Helper()
	channel := model.Channel{
		Type: constant.ChannelTypeOpenAI, Key: "profile-key", Status: common.ChannelStatusEnabled,
		Name: "profile-channel", Models: modelName, Group: ImageStudioTokenGroup, CreatedTime: time.Now().Unix(),
	}
	require.NoError(t, model.DB.Create(&channel).Error)
	priority := int64(0)
	require.NoError(t, model.DB.Create(&model.Ability{
		Group: ImageStudioTokenGroup, Model: modelName, ChannelId: channel.Id, Enabled: true, Priority: &priority,
	}).Error)
}

func TestImageModelProfileCannotPublishUnboundedTieredBilling(t *testing.T) {
	db := setupImageStudioTokenTest(t)
	seedImageStudioAbility(t, "gpt-image-1")
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		saved[key] = value
		return nil
	}))
	t.Cleanup(func() { require.NoError(t, config.GlobalConfig.LoadFromDB(saved)) })
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": `{"gpt-image-1":"tiered_expr"}`,
		"billing_setting.billing_expr": `{"gpt-image-1":"img_o * 40"}`,
	}))

	_, err := CreateImageModelProfile(context.Background(), db, imageModelProfileInput(1))
	require.ErrorIs(t, err, ErrImageModelBillingUnsupported)
	input := imageModelProfileInput(1)
	input.Enabled = false
	created, err := CreateImageModelProfile(context.Background(), db, input)
	require.NoError(t, err)
	require.False(t, created.Enabled)
}

func imageModelProfileInput(version int) ImageModelProfileInput {
	specification := validImageModelSpec()
	specification.Version = version
	return ImageModelProfileInput{
		Model: "gpt-image-1", DisplayName: "GPT Image", Description: "Image generation",
		ProviderLabel: "OpenAI", Specification: specification,
		DefaultParameters: map[string]any{"size": "1024x1024", "count": 1}, Enabled: true,
	}
}

func TestImageModelProfileRejectsUnpricedSizeOptions(t *testing.T) {
	originalPolicy := image_pricing_setting.JSON()
	t.Cleanup(func() {
		require.NoError(t, image_pricing_setting.UpdateByJSONString(originalPolicy))
	})
	policy := image_pricing_setting.DefaultConfig()
	policy.Enabled = true
	encoded, err := common.Marshal(policy)
	require.NoError(t, err)
	require.NoError(t, image_pricing_setting.UpdateByJSONString(string(encoded)))

	input := imageModelProfileInput(1)
	input.Model = "gpt-image-2"
	input.Specification.Parameters[0].Options = append(
		input.Specification.Parameters[0].Options,
		ImageParameterOption{Label: "4K landscape", Value: "3840x2160"},
	)
	_, _, _, err = normalizeImageModelProfileInput(input)
	require.NoError(t, err)

	input.Specification.Parameters[0].Options = append(
		input.Specification.Parameters[0].Options,
		ImageParameterOption{Label: "Automatic", Value: "auto"},
	)

	_, _, _, err = normalizeImageModelProfileInput(input)
	assert.ErrorIs(t, err, ErrInvalidImageModelSpec)
}

func TestCreateAndUpdateImageModelProfileRequireVersionedSpecification(t *testing.T) {
	db := setupImageStudioTokenTest(t)
	seedImageStudioAbility(t, "gpt-image-1")

	created, err := CreateImageModelProfile(context.Background(), db, imageModelProfileInput(1))
	require.NoError(t, err)
	assert.Equal(t, "gpt-image-1", created.Model)
	assert.True(t, created.Enabled)

	metadataOnly := imageModelProfileInput(1)
	metadataOnly.Description = "Updated description"
	updated, err := UpdateImageModelProfile(context.Background(), db, created.ID, metadataOnly)
	require.NoError(t, err)
	assert.Equal(t, "Updated description", updated.Description)

	changedSpec := imageModelProfileInput(1)
	changedSpec.Specification.Parameters[0].Options[0].Value = "1024x1536"
	changedSpec.DefaultParameters["size"] = "1024x1536"
	_, err = UpdateImageModelProfile(context.Background(), db, created.ID, changedSpec)
	assert.ErrorIs(t, err, ErrInvalidImageModelSpec)

	changedSpec.Specification.Version = 2
	updated, err = UpdateImageModelProfile(context.Background(), db, created.ID, changedSpec)
	require.NoError(t, err)
	assert.Equal(t, 2, updated.SpecificationVersion)
}

func TestDeleteImageModelProfileRejectsReferencedProfile(t *testing.T) {
	db := setupImageStudioTokenTest(t)
	seedImageStudioAbility(t, "gpt-image-1")
	created, err := CreateImageModelProfile(context.Background(), db, imageModelProfileInput(1))
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.KKAIImageGeneration{
		UserID: 42, TokenID: 1, ModelProfileID: created.ID, SpecificationVersion: 1,
		Model: created.Model, Prompt: "prompt", Parameters: `{}`, RequestHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RequestID: "profile-in-use", Status: model.ImageGenerationStatusSucceeded, RequestedCount: 1,
		StartedAt: time.Now().Unix(), CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}).Error)

	err = DeleteImageModelProfile(context.Background(), db, created.ID)
	assert.ErrorIs(t, err, ErrImageModelProfileInUse)
}
