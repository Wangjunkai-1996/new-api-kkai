package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newVideoStudioTokenTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:video-token-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.KKAIVideoModelProfile{}))
	return db
}

func setupVideoStudioTokenTest(t *testing.T) *gorm.DB {
	t.Helper()
	db := newVideoStudioTokenTestDB(t)
	previousDB := model.DB
	previousRedisEnabled := common.RedisEnabled
	previousUsableGroups := setting.UserUsableGroups2JSONString()
	previousGroupRatios := ratio_setting.GroupRatio2JSONString()
	previousSpecialGroups := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.MarshalJSONString()
	model.DB = db
	common.RedisEnabled = false
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","Seedance 视频":"Seedance 视频"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"Seedance 视频":1}`))
	require.NoError(t, ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.UnmarshalJSON([]byte(`{}`)))
	t.Cleanup(func() {
		model.DB = previousDB
		common.RedisEnabled = previousRedisEnabled
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroupRatios))
		require.NoError(t, ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.UnmarshalJSON([]byte(previousSpecialGroups)))
	})
	require.NoError(t, db.Create(&model.User{
		Id: 42, Username: "video-user", Password: "password", DisplayName: "Video User",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default",
	}).Error)
	for index, modelName := range []string{"video-model-b", "video-model-a"} {
		require.NoError(t, db.Create(&model.KKAIVideoModelProfile{
			Model: modelName, DisplayName: modelName, Description: "test", SpecificationVersion: 1,
			Specification: `{}`, DefaultParameters: `{}`, Enabled: true, SortOrder: index,
			CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
		}).Error)
	}
	return db
}

func createVideoStudioTestToken(t *testing.T, db *gorm.DB, mutate func(*model.Token)) model.Token {
	t.Helper()
	token := model.Token{
		UserId: 42, Key: fmt.Sprintf("video-key-%d", time.Now().UnixNano()), Status: common.TokenStatusEnabled,
		Name: "existing video token", CreatedTime: time.Now().Unix(), AccessedTime: time.Now().Unix(),
		ExpiredTime: -1, UnlimitedQuota: true, ModelLimitsEnabled: true,
		ModelLimits: "video-model-a", Group: VideoStudioTokenGroup,
	}
	if mutate != nil {
		mutate(&token)
	}
	require.NoError(t, db.Create(&token).Error)
	return token
}

func TestGetVideoStudioTokenStatusSelectsOnlyUsableTokenForModel(t *testing.T) {
	db := setupVideoStudioTokenTest(t)
	ctx := context.Background()

	status, err := GetVideoStudioTokenStatus(ctx, db, 42, "video-model-a", "192.0.2.1")
	require.NoError(t, err)
	assert.Equal(t, VideoStudioTokenGroup, status.RequiredGroup)
	assert.False(t, status.HasUsableToken)
	assert.True(t, status.CanCreate)
	assert.Equal(t, VideoStudioTokenStatusMissing, status.Status)
	assert.Nil(t, status.Token)

	createVideoStudioTestToken(t, db, nil)
	status, err = GetVideoStudioTokenStatus(ctx, db, 42, "video-model-a", "192.0.2.1")
	require.NoError(t, err)
	assert.True(t, status.HasUsableToken)
	assert.True(t, status.CanCreate)
	assert.Equal(t, VideoStudioTokenStatusReady, status.Status)
	require.NotNil(t, status.Token)
	assert.Positive(t, status.Token.ID)
	assert.Equal(t, VideoStudioTokenGroup, status.Token.Group)

	status, err = GetVideoStudioTokenStatus(ctx, db, 42, "video-model-b", "192.0.2.1")
	require.NoError(t, err)
	assert.False(t, status.HasUsableToken)
	assert.True(t, status.CanCreate)
	assert.Nil(t, status.Token)
}

func TestEnsureVideoStudioTokenIsIdempotentAndUsesEnabledModelWhitelist(t *testing.T) {
	db := setupVideoStudioTokenTest(t)
	ctx := context.Background()

	first, err := EnsureVideoStudioToken(ctx, db, 42, "video-model-a", "192.0.2.1")
	require.NoError(t, err)
	assert.True(t, first.HasUsableToken)
	assert.True(t, first.Created)
	require.NotNil(t, first.Token)
	assert.Positive(t, first.Token.ID)

	second, err := EnsureVideoStudioToken(ctx, db, 42, "video-model-a", "192.0.2.1")
	require.NoError(t, err)
	require.NotNil(t, second.Token)
	assert.Equal(t, first.Token.ID, second.Token.ID)
	assert.False(t, second.Created)

	var tokens []model.Token
	require.NoError(t, db.Where("user_id = ?", 42).Find(&tokens).Error)
	require.Len(t, tokens, 1)
	token := tokens[0]
	assert.Equal(t, "视频工作室", token.Name)
	assert.NotEmpty(t, token.Key)
	assert.Equal(t, common.TokenStatusEnabled, token.Status)
	assert.Equal(t, int64(-1), token.ExpiredTime)
	assert.True(t, token.UnlimitedQuota)
	assert.True(t, token.ModelLimitsEnabled)
	assert.Equal(t, "video-model-a,video-model-b", token.ModelLimits)
	assert.Equal(t, VideoStudioTokenGroup, token.Group)
	assert.False(t, token.CrossGroupRetry)
}

func TestEnsureVideoStudioTokenConcurrentRequestsConvergeOnOneToken(t *testing.T) {
	db := setupVideoStudioTokenTest(t)
	start := make(chan struct{})
	results := make(chan VideoStudioTokenEnsureResult, 2)
	errors := make(chan error, 2)
	var waitGroup sync.WaitGroup
	for range 2 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			result, err := EnsureVideoStudioToken(context.Background(), db, 42, "video-model-a", "192.0.2.1")
			results <- result
			errors <- err
		}()
	}
	close(start)
	waitGroup.Wait()
	close(results)
	close(errors)

	for err := range errors {
		require.NoError(t, err)
	}
	tokenIDs := map[int]bool{}
	for result := range results {
		require.NotNil(t, result.Token)
		tokenIDs[result.Token.ID] = true
	}
	assert.Len(t, tokenIDs, 1)
	var count int64
	require.NoError(t, db.Model(&model.Token{}).Where("user_id = ?", 42).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestValidateVideoStudioTokenEnforcesOwnershipStateGroupAndModel(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*model.Token)
		userID    int
		modelName string
		wantErr   error
	}{
		{name: "valid", userID: 42, modelName: "video-model-a"},
		{name: "foreign token", userID: 99, modelName: "video-model-a", wantErr: ErrVideoStudioTokenInvalid},
		{name: "disabled", userID: 42, modelName: "video-model-a", mutate: func(token *model.Token) {
			token.Status = common.TokenStatusDisabled
		}, wantErr: ErrVideoStudioTokenInvalid},
		{name: "expired", userID: 42, modelName: "video-model-a", mutate: func(token *model.Token) {
			token.ExpiredTime = time.Now().Add(-time.Minute).Unix()
		}, wantErr: ErrVideoStudioTokenInvalid},
		{name: "exhausted", userID: 42, modelName: "video-model-a", mutate: func(token *model.Token) {
			token.UnlimitedQuota = false
			token.RemainQuota = 0
		}, wantErr: ErrVideoStudioTokenInvalid},
		{name: "finite quota", userID: 42, modelName: "video-model-a", mutate: func(token *model.Token) {
			token.UnlimitedQuota = false
			token.RemainQuota = 1
		}},
		{name: "wrong group", userID: 42, modelName: "video-model-a", mutate: func(token *model.Token) {
			token.Group = "default"
		}, wantErr: ErrVideoStudioTokenGroupInvalid},
		{name: "model forbidden", userID: 42, modelName: "video-model-b", wantErr: ErrVideoStudioTokenModelForbidden},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := setupVideoStudioTokenTest(t)
			token := createVideoStudioTestToken(t, db, test.mutate)
			validated, err := ValidateVideoStudioToken(
				context.Background(), db, test.userID, token.Id, test.modelName, "192.0.2.1",
			)
			if test.wantErr != nil {
				require.ErrorIs(t, err, test.wantErr)
				assert.Nil(t, validated)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, token.Id, validated.Id)
		})
	}
}

func TestGetVideoStudioTokenStatusReportsUnavailableGroup(t *testing.T) {
	db := setupVideoStudioTokenTest(t)
	require.NoError(t, ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.UnmarshalJSON([]byte(`{
		"restricted":{"-:Seedance 视频":"hidden"}
	}`)))
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 42).Update("group", "restricted").Error)

	status, err := GetVideoStudioTokenStatus(context.Background(), db, 42, "video-model-a", "192.0.2.1")
	require.NoError(t, err)
	assert.False(t, status.HasUsableToken)
	assert.False(t, status.CanCreate)
	assert.Equal(t, VideoStudioTokenStatusGroupUnavailable, status.Status)
	assert.Nil(t, status.Token)
}

func TestVideoStudioTokenStatusUsesCurrentUserInsteadOfStaleSessionGroup(t *testing.T) {
	db := setupVideoStudioTokenTest(t)
	createVideoStudioTestToken(t, db, nil)
	require.NoError(t, ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.UnmarshalJSON([]byte(`{
		"restricted":{"-:Seedance 视频":"hidden"}
	}`)))
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 42).Update("group", "restricted").Error)

	status, err := GetVideoStudioTokenStatus(context.Background(), db, 42, "video-model-a", "192.0.2.1")
	require.NoError(t, err)
	assert.False(t, status.HasUsableToken)
	assert.False(t, status.CanCreate)
	assert.Equal(t, VideoStudioTokenStatusGroupUnavailable, status.Status)
	require.ErrorIs(
		t, ensureVideoStudioTokenError(context.Background(), db, 42, "video-model-a", "192.0.2.1"),
		ErrVideoStudioTokenGroupUnavailable,
	)
}

func TestVideoStudioTokenStatusReportsStableUnavailableReasons(t *testing.T) {
	t.Run("token limit", func(t *testing.T) {
		db := setupVideoStudioTokenTest(t)
		previousMax := operation_setting.GetTokenSetting().MaxUserTokens
		operation_setting.GetTokenSetting().MaxUserTokens = 0
		t.Cleanup(func() { operation_setting.GetTokenSetting().MaxUserTokens = previousMax })

		status, err := GetVideoStudioTokenStatus(context.Background(), db, 42, "video-model-a", "192.0.2.1")
		require.NoError(t, err)
		assert.False(t, status.CanCreate)
		assert.Equal(t, VideoStudioTokenStatusLimitReached, status.Status)
	})

	t.Run("model not allowed", func(t *testing.T) {
		db := setupVideoStudioTokenTest(t)
		status, err := GetVideoStudioTokenStatus(context.Background(), db, 42, "missing-video-model", "192.0.2.1")
		require.NoError(t, err)
		assert.False(t, status.CanCreate)
		assert.Equal(t, VideoStudioTokenStatusModelsUnavailable, status.Status)
	})

	t.Run("unlimited token cannot bypass disabled model", func(t *testing.T) {
		db := setupVideoStudioTokenTest(t)
		createVideoStudioTestToken(t, db, func(token *model.Token) {
			token.ModelLimitsEnabled = false
			token.ModelLimits = ""
		})
		status, err := GetVideoStudioTokenStatus(context.Background(), db, 42, "missing-video-model", "192.0.2.1")
		require.NoError(t, err)
		assert.False(t, status.HasUsableToken)
		assert.False(t, status.CanCreate)
		assert.Equal(t, VideoStudioTokenStatusModelsUnavailable, status.Status)
	})

	t.Run("database error", func(t *testing.T) {
		db := setupVideoStudioTokenTest(t)
		require.NoError(t, db.Migrator().DropTable(&model.Token{}))
		_, err := GetVideoStudioTokenStatus(context.Background(), db, 42, "video-model-a", "192.0.2.1")
		require.Error(t, err)
	})
}

func TestVideoStudioTokenRejectsDisabledCurrentUser(t *testing.T) {
	db := setupVideoStudioTokenTest(t)
	token := createVideoStudioTestToken(t, db, nil)
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 42).Update("status", common.UserStatusDisabled).Error)

	status, err := GetVideoStudioTokenStatus(context.Background(), db, 42, "video-model-a", "192.0.2.1")
	require.NoError(t, err)
	assert.Equal(t, VideoStudioTokenStatusGroupUnavailable, status.Status)
	assert.False(t, status.HasUsableToken)
	assert.False(t, status.CanCreate)
	require.ErrorIs(t, ensureVideoStudioTokenError(context.Background(), db, 42, "video-model-a", "192.0.2.1"), ErrVideoStudioTokenGroupUnavailable)
	_, err = ValidateVideoStudioToken(context.Background(), db, 42, token.Id, "video-model-a", "192.0.2.1")
	require.ErrorIs(t, err, ErrVideoStudioTokenGroupUnavailable)
}

func TestVideoStudioTokenHonorsClientIPRestrictions(t *testing.T) {
	db := setupVideoStudioTokenTest(t)
	allowIPs := "10.0.0.0/8"
	token := createVideoStudioTestToken(t, db, func(token *model.Token) { token.AllowIps = &allowIPs })

	inside, err := GetVideoStudioTokenStatus(context.Background(), db, 42, "video-model-a", "10.2.3.4")
	require.NoError(t, err)
	assert.True(t, inside.HasUsableToken)

	outside, err := GetVideoStudioTokenStatus(context.Background(), db, 42, "video-model-a", "203.0.113.8")
	require.NoError(t, err)
	assert.False(t, outside.HasUsableToken)
	require.ErrorIs(
		t,
		func() error {
			_, err := ValidateVideoStudioToken(
				context.Background(), db, 42, token.Id, "video-model-a", "203.0.113.8",
			)
			return err
		}(),
		ErrVideoStudioTokenIPForbidden,
	)
}

func TestEnsureVideoStudioTokenDoesNotDeadlockWithOneConnectionAndInvalidCandidates(t *testing.T) {
	db := setupVideoStudioTokenTest(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	createVideoStudioTestToken(t, db, func(token *model.Token) {
		token.ExpiredTime = time.Now().Add(-time.Minute).Unix()
	})
	createVideoStudioTestToken(t, db, func(token *model.Token) {
		token.UnlimitedQuota = false
		token.RemainQuota = 0
	})

	type ensureResult struct {
		result VideoStudioTokenEnsureResult
		err    error
	}
	completed := make(chan ensureResult, 1)
	go func() {
		result, err := EnsureVideoStudioToken(context.Background(), db, 42, "video-model-a", "192.0.2.1")
		completed <- ensureResult{result: result, err: err}
	}()

	select {
	case outcome := <-completed:
		require.NoError(t, outcome.err)
		require.True(t, outcome.result.Created)
		require.NotNil(t, outcome.result.Token)
	case <-time.After(2 * time.Second):
		t.Fatal("video studio token creation deadlocked with one database connection")
	}
}

func ensureVideoStudioTokenError(ctx context.Context, db *gorm.DB, userID int, modelName string, clientIP string) error {
	_, err := EnsureVideoStudioToken(ctx, db, userID, modelName, clientIP)
	return err
}
