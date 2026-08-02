package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupImageStudioTokenTest(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:image-token-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{},
		&model.KKAIImageModelProfile{}, &model.KKAIImageSample{}, &model.KKAIImageGeneration{}, &model.KKAIImageAsset{},
	))
	previousDB := model.DB
	previousRedisEnabled := common.RedisEnabled
	previousUsableGroups := setting.UserUsableGroups2JSONString()
	previousGroupRatios := ratio_setting.GroupRatio2JSONString()
	previousSpecialGroups := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.MarshalJSONString()
	model.DB = db
	common.RedisEnabled = false
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","图片工作室":"图片工作室"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"图片工作室":1}`))
	require.NoError(t, ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.UnmarshalJSON([]byte(`{}`)))
	t.Cleanup(func() {
		model.DB = previousDB
		common.RedisEnabled = previousRedisEnabled
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroupRatios))
		require.NoError(t, ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.UnmarshalJSON([]byte(previousSpecialGroups)))
	})
	require.NoError(t, db.Create(&model.User{
		Id: 42, Username: "image-user", Password: "password", DisplayName: "Image User",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default",
	}).Error)
	return db
}

func seedImageStudioModel(t *testing.T, db *gorm.DB, modelName string, channelType int) {
	t.Helper()
	channel := model.Channel{
		Type: channelType, Key: "test-key", Status: common.ChannelStatusEnabled, Name: modelName,
		Models: modelName, Group: ImageStudioTokenGroup, CreatedTime: time.Now().Unix(),
	}
	require.NoError(t, db.Create(&channel).Error)
	priority := int64(0)
	require.NoError(t, db.Create(&model.Ability{
		Group: ImageStudioTokenGroup, Model: modelName, ChannelId: channel.Id,
		Enabled: true, Priority: &priority,
	}).Error)
	require.NoError(t, db.Create(&model.KKAIImageModelProfile{
		Model: modelName, DisplayName: modelName, Description: "test", SpecificationVersion: 1,
		Specification: `{"version":1,"parameters":[]}`, DefaultParameters: `{}`, Enabled: true,
		CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}).Error)
}

func TestEnabledImageStudioModelsFiltersNonImageEndpoints(t *testing.T) {
	db := setupImageStudioTokenTest(t)
	seedImageStudioModel(t, db, "gpt-image-1", constant.ChannelTypeOpenAI)
	seedImageStudioModel(t, db, "chat-only-model", constant.ChannelTypeOpenAI)

	models, err := enabledConfiguredImageStudioModelsForGroup(context.Background(), db, ImageStudioTokenGroup)
	require.NoError(t, err)
	assert.Equal(t, []string{"gpt-image-1"}, models)
}

func TestEnsureImageStudioTokenIsIdempotentAndManaged(t *testing.T) {
	db := setupImageStudioTokenTest(t)
	seedImageStudioModel(t, db, "gpt-image-1", constant.ChannelTypeOpenAI)

	first, err := EnsureImageStudioToken(context.Background(), db, 42, "gpt-image-1", "192.0.2.1")
	require.NoError(t, err)
	require.True(t, first.Created)
	require.NotNil(t, first.Token)
	second, err := EnsureImageStudioToken(context.Background(), db, 42, "gpt-image-1", "192.0.2.1")
	require.NoError(t, err)
	require.NotNil(t, second.Token)
	assert.False(t, second.Created)
	assert.Equal(t, first.Token.ID, second.Token.ID)

	var tokens []model.Token
	require.NoError(t, db.Where("user_id = ?", 42).Find(&tokens).Error)
	require.Len(t, tokens, 1)
	assert.Equal(t, imageStudioTokenName, tokens[0].Name)
	assert.Equal(t, ImageStudioTokenGroup, tokens[0].Group)
	assert.True(t, tokens[0].UnlimitedQuota)
	assert.False(t, tokens[0].ModelLimitsEnabled)
	assert.False(t, tokens[0].CrossGroupRetry)
}

func TestValidateImageStudioTokenRejectsOrdinarySameGroupToken(t *testing.T) {
	db := setupImageStudioTokenTest(t)
	seedImageStudioModel(t, db, "gpt-image-1", constant.ChannelTypeOpenAI)
	token := model.Token{
		UserId: 42, Key: "ordinary-key", Status: common.TokenStatusEnabled, Name: "ordinary",
		CreatedTime: time.Now().Unix(), AccessedTime: time.Now().Unix(), ExpiredTime: -1,
		UnlimitedQuota: true, Group: ImageStudioTokenGroup,
	}
	require.NoError(t, db.Create(&token).Error)

	_, err := ValidateImageStudioToken(context.Background(), db, 42, token.Id, "gpt-image-1", "192.0.2.1")
	assert.ErrorIs(t, err, ErrImageStudioTokenInvalid)
}
