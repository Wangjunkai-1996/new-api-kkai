package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelSelectionExcludesExhaustedPoolsPerRequest(t *testing.T) {
	db := newImageStudioChannelSelectionTestDB(t)
	const group, modelName = "pool-selection", "gpt-5.5"
	first := seedImageStudioSelectionChannel(t, db, group, modelName, constant.ChannelTypeOpenAI, 30)
	peer := seedImageStudioSelectionChannel(t, db, group, modelName, constant.ChannelTypeOpenAI, 30)
	backup := seedImageStudioSelectionChannel(t, db, group, modelName, constant.ChannelTypeOpenAI, 10)
	seedImageStudioSelectionChannel(t, db, "private-group", modelName, constant.ChannelTypeOpenAI, 50)
	seedImageStudioSelectionChannel(t, db, group, "another-model", constant.ChannelTypeOpenAI, 50)
	seedImageStudioSelectionChannel(t, db, group, modelName, constant.ChannelTypeAdvancedCustom, 40)
	for _, memoryCache := range []bool{false, true} {
		t.Run(fmt.Sprintf("memory_cache_%t", memoryCache), func(t *testing.T) {
			common.MemoryCacheEnabled = memoryCache
			if memoryCache {
				require.NoError(t, model.SyncChannelCacheOnce())
			}
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			param := &RetryParam{Ctx: ctx, TokenGroup: group, ModelName: modelName, RequestPath: ctx.Request.URL.Path,
				Retry: common.GetPointer(1), ExcludedChannelIDs: []int{first.Id}}
			selected, selectedGroup, err := CacheGetRandomSatisfiedChannel(param)
			require.NoError(t, err)
			require.NotNil(t, selected)
			assert.Equal(t, group, selectedGroup)
			assert.Equal(t, peer.Id, selected.Id, "a healthy peer remains ahead of a lower-priority backup")
			param.ExcludedChannelIDs = append(param.ExcludedChannelIDs, peer.Id)
			param.IncreaseRetry()
			selected, _, err = CacheGetRandomSatisfiedChannel(param)
			require.NoError(t, err)
			require.NotNil(t, selected)
			assert.Equal(t, backup.Id, selected.Id)
			param.ExcludedChannelIDs = append(param.ExcludedChannelIDs, backup.Id)
			SetImageStudioReferenceCount(ctx, 2)
			selected, _, err = CacheGetRandomSatisfiedChannel(param)
			require.NoError(t, err)
			assert.Nil(t, selected, "exhaustion must not cross the requested group, model or path")
			param.ExcludedChannelIDs = nil
			param.SetRetry(100)
			selected, _, err = CacheGetRandomSatisfiedChannel(param)
			require.NoError(t, err)
			require.NotNil(t, selected)
			assert.Equal(t, backup.Id, selected.Id, "another request may use a previously excluded channel")
		})
	}
}

func TestExhaustedPoolAutoGroupRetryBoundaries(t *testing.T) {
	previousAutoGroups := setting.AutoGroups2JsonString()
	previousProfiles := setting.AutoGroupProfiles2JsonString()
	previousUsableGroups := setting.UserUsableGroups2JSONString()
	previousRetries := common.RetryTimes
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(previousAutoGroups))
		require.NoError(t, setting.UpdateAutoGroupProfilesByJsonString(previousProfiles))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousUsableGroups))
		common.RetryTimes = previousRetries
	})
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["pool-primary","pool-backup"]`))
	require.NoError(t, setting.UpdateAutoGroupProfilesByJsonString(`{"auto2":["pool-primary","pool-backup"]}`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"pool-primary":"Primary","pool-backup":"Backup"}`))
	common.RetryTimes = 2

	for _, memoryCache := range []bool{false, true} {
		for _, tokenGroup := range []string{"auto", "auto2"} {
			for _, tt := range []struct {
				name            string
				crossGroupRetry bool
				poolOnly        bool
				disableFirst    bool
			}{
				{"mixed failures with cross-group retry", true, false, false},
				{"mixed failures without cross-group retry", false, false, false},
				{"empty pool with cross-group retry", true, true, false},
				{"empty pool without cross-group retry", false, true, false},
				{"disabled channel with cross-group retry", true, true, true},
				{"disabled channel without cross-group retry", false, true, true},
			} {
				t.Run(fmt.Sprintf("memory_cache_%t/%s/%s", memoryCache, tokenGroup, tt.name), func(t *testing.T) {
					db := newImageStudioChannelSelectionTestDB(t)
					const modelName = "gpt-5.5"
					first := seedImageStudioSelectionChannel(t, db, "pool-primary", modelName, constant.ChannelTypeOpenAI, 30)
					wantIDs := []int{first.Id}
					if !tt.poolOnly {
						second := seedImageStudioSelectionChannel(t, db, "pool-primary", modelName, constant.ChannelTypeOpenAI, 20)
						third := seedImageStudioSelectionChannel(t, db, "pool-primary", modelName, constant.ChannelTypeOpenAI, 10)
						wantIDs = append(wantIDs, second.Id, third.Id)
					}
					backup := seedImageStudioSelectionChannel(t, db, "pool-backup", modelName, constant.ChannelTypeOpenAI, 30)
					seedImageStudioSelectionChannel(t, db, "pool-backup", modelName, constant.ChannelTypeOpenAI, 10)
					if tt.crossGroupRetry {
						wantIDs = append(wantIDs, backup.Id)
					}
					common.MemoryCacheEnabled = memoryCache
					if memoryCache {
						require.NoError(t, model.SyncChannelCacheOnce())
					}
					ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
					ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
					common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, tt.crossGroupRetry)
					param := &RetryParam{Ctx: ctx, TokenGroup: tokenGroup, ModelName: modelName, RequestPath: ctx.Request.URL.Path}
					var gotIDs []int
					for ; param.GetRetry() <= common.RetryTimes; param.IncreaseRetry() {
						selected, selectedGroup, err := CacheGetRandomSatisfiedChannel(param)
						require.NoError(t, err)
						if selected == nil {
							break
						}
						assert.Equal(t, selected.Group, selectedGroup)
						assert.Equal(t, selected.Group, common.GetContextKeyString(ctx, constant.ContextKeyAutoGroup))
						gotIDs = append(gotIDs, selected.Id)
						require.LessOrEqual(t, len(gotIDs), len(wantIDs), "each group must retain its attempt budget")
						if selected.Group == "pool-backup" {
							break
						}
						if selected.Id == first.Id {
							if tt.disableFirst {
								require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", selected.Id).Update("status", common.ChannelStatusManuallyDisabled).Error)
								require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", selected.Id).Update("enabled", false).Error)
								if memoryCache {
									require.NoError(t, model.SyncChannelCacheOnce())
								}
							} else {
								param.ExcludedChannelIDs = append(param.ExcludedChannelIDs, selected.Id)
							}
						} else {
							param.PriorityRetry++
						}
					}
					assert.Equal(t, wantIDs, gotIDs, "pool failures retain the attempt budget and cross-group permission")
				})
			}
		}
	}
}

func TestAutoGroupSelectionKeepsProfilesAndRequestStateIndependent(t *testing.T) {
	previousAutoGroups := setting.AutoGroups2JsonString()
	previousProfiles := setting.AutoGroupProfiles2JsonString()
	previousUsableGroups := setting.UserUsableGroups2JSONString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(previousAutoGroups))
		require.NoError(t, setting.UpdateAutoGroupProfilesByJsonString(previousProfiles))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousUsableGroups))
	})
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["legacy"]`))
	require.NoError(t, setting.UpdateAutoGroupProfilesByJsonString(`{"auto2":["unavailable","private","profile"],"auto3":["private"]}`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"legacy":"Legacy","unavailable":"Unavailable","profile":"Profile"}`))

	for _, memoryCache := range []bool{false, true} {
		t.Run(fmt.Sprintf("memory_cache_%t", memoryCache), func(t *testing.T) {
			db := newImageStudioChannelSelectionTestDB(t)
			const modelName = "gpt-5.5"
			legacy := seedImageStudioSelectionChannel(t, db, "legacy", modelName, constant.ChannelTypeOpenAI, 10)
			profile := seedImageStudioSelectionChannel(t, db, "profile", modelName, constant.ChannelTypeOpenAI, 10)
			seedImageStudioSelectionChannel(t, db, "private", modelName, constant.ChannelTypeOpenAI, 100)
			common.MemoryCacheEnabled = memoryCache
			if memoryCache {
				require.NoError(t, model.SyncChannelCacheOnce())
			}

			for _, tc := range []struct {
				tokenGroup string
				channelID  int
				group      string
			}{
				{"auto2", profile.Id, "profile"},
				{"auto", legacy.Id, "legacy"},
				{"auto2", profile.Id, "profile"},
				{"auto3", 0, ""},
			} {
				ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
				ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				common.SetContextKey(ctx, constant.ContextKeyTokenGroup, tc.tokenGroup)
				param := &RetryParam{Ctx: ctx, TokenGroup: tc.tokenGroup, ModelName: modelName, RequestPath: ctx.Request.URL.Path}
				selected, selectedGroup, err := CacheGetRandomSatisfiedChannel(param)
				if tc.channelID == 0 {
					require.ErrorContains(t, err, "not enabled")
					assert.Nil(t, selected)
					assert.Empty(t, common.GetContextKeyString(ctx, constant.ContextKeyAutoGroup))
					continue
				}
				require.NoError(t, err)
				require.NotNil(t, selected)
				assert.Equal(t, tc.channelID, selected.Id)
				assert.Equal(t, tc.group, selectedGroup)
				assert.Equal(t, tc.group, common.GetContextKeyString(ctx, constant.ContextKeyAutoGroup))
				assert.Equal(t, tc.tokenGroup, common.GetContextKeyString(ctx, constant.ContextKeyTokenGroup))
			}
		})
	}
}
