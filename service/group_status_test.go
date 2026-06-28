package service

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupGroupStatusServiceTest(t *testing.T) *gorm.DB {
	t.Helper()

	prepareGroupStatusModelColumns(t)
	common.RedisEnabled = false
	perfmetrics.ResetForTest()
	t.Cleanup(perfmetrics.ResetForTest)

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.PerfMetric{}, &model.Log{}))

	resetGroupStatusSettings(t)
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func prepareGroupStatusModelColumns(t *testing.T) {
	t.Helper()

	originalIsMasterNode := common.IsMasterNode
	originalSQLitePath := common.SQLitePath
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalSQLDSN, hadSQLDSN := os.LookupEnv("SQL_DSN")

	common.IsMasterNode = false
	common.SQLitePath = fmt.Sprintf("file:%s_init?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, os.Setenv("SQL_DSN", "local"))
	require.NoError(t, model.InitDB())
	if model.DB != nil {
		sqlDB, err := model.DB.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	}

	common.IsMasterNode = originalIsMasterNode
	common.SQLitePath = originalSQLitePath
	common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
	if hadSQLDSN {
		require.NoError(t, os.Setenv("SQL_DSN", originalSQLDSN))
	} else {
		require.NoError(t, os.Unsetenv("SQL_DSN"))
	}
}

func resetGroupStatusSettings(t *testing.T) {
	t.Helper()

	userUsableGroups := setting.UserUsableGroups2JSONString()
	autoGroups := setting.AutoGroups2JsonString()
	groupRatios := ratio_setting.GroupRatio2JSONString()
	groupSpecialUsableGroups := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.MarshalJSONString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(userUsableGroups))
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(autoGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(groupRatios))
		require.NoError(t, ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.UnmarshalJSON([]byte(groupSpecialUsableGroups)))
	})

	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{
		"default":"Default",
		"vip":"VIP",
		"slow":"Slow",
		"down":"Down",
		"empty":"Empty",
		"auto":"Auto"
	}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{
		"default":1,
		"vip":1,
		"slow":1,
		"down":1,
		"empty":1
	}`))
	require.NoError(t, ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.UnmarshalJSON([]byte(`{}`)))
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["default","vip"]`))
}

func TestGroupStatusServiceClassifiesGroups(t *testing.T) {
	db := setupGroupStatusServiceTest(t)
	insertGroupAbility(t, db, "default", "gpt-4o", 1, true)
	insertGroupAbility(t, db, "vip", "gpt-4o", 2, true)
	insertGroupAbility(t, db, "slow", "gpt-4o", 3, true)
	insertGroupAbility(t, db, "down", "gpt-4o", 4, true)
	insertGroupAbility(t, db, "empty", "gpt-4o", 5, false)

	nowBucket := time.Now().Unix()
	insertGroupMetric(t, db, "default", 120, 120, 1200, 300)
	insertGroupMetric(t, db, "vip", 80, 70, 1000, 300)
	insertGroupMetric(t, db, "slow", 80, 80, 20000, 6000)
	insertGroupMetricAt(t, db, "down", "gpt-4o", nowBucket, 80, 10, 1000, 300)

	result, err := GetUserGroupStatuses(GroupStatusRequest{
		UsableGroups: map[string]string{
			"default": "Default",
			"vip":     "VIP",
			"slow":    "Slow",
			"down":    "Down",
			"empty":   "Empty",
		},
		Hours: 6,
	})
	require.NoError(t, err)

	byGroup := groupStatusByName(result.Groups)
	require.Equal(t, GroupHealthOperational, byGroup["default"].Status)
	require.Equal(t, GroupHealthConfidenceHigh, byGroup["default"].Confidence)
	require.Equal(t, GroupConfidenceExcellent, byGroup["default"].ConfidenceStatus)
	require.Equal(t, GroupExperienceLightning, byGroup["default"].ExperienceLabel)
	require.Equal(t, GroupHealthDegraded, byGroup["vip"].Status)
	require.Equal(t, GroupConfidenceUnstable, byGroup["vip"].ConfidenceStatus)
	require.Equal(t, GroupHealthOperational, byGroup["slow"].Status)
	require.Equal(t, GroupConfidenceExcellent, byGroup["slow"].ConfidenceStatus)
	require.Equal(t, GroupExperienceNormal, byGroup["slow"].ExperienceLabel)
	require.Equal(t, GroupHealthOutage, byGroup["down"].Status)
	require.Equal(t, GroupConfidenceUnavailable, byGroup["down"].ConfidenceStatus)
	require.Equal(t, GroupHealthOutage, byGroup["empty"].Status)
	require.Equal(t, GroupConfidenceUnavailable, byGroup["empty"].ConfidenceStatus)
	require.Equal(t, int64(0), byGroup["empty"].AvailableModelCount)
}

func TestGroupStatusServiceKeepsHighSuccessGroupsPositiveDespiteLongLatency(t *testing.T) {
	db := setupGroupStatusServiceTest(t)
	insertGroupAbility(t, db, "default", "gpt-4o", 1, true)
	insertGroupMetric(t, db, "default", 2000, 1999, 45000, 1500)

	result, err := GetUserGroupStatuses(GroupStatusRequest{
		UsableGroups: map[string]string{"default": "Default"},
		Hours:        6,
	})
	require.NoError(t, err)

	entry := groupStatusByName(result.Groups)["default"]
	require.Equal(t, 99.95, entry.SuccessRate)
	require.Equal(t, GroupHealthOperational, entry.Status)
	require.Equal(t, GroupConfidenceExcellent, entry.ConfidenceStatus)
	require.Equal(t, GroupRecommendationBest, entry.RecommendationLevel)
	require.Equal(t, GroupExperienceLightning, entry.ExperienceLabel)
	require.NotEqual(t, GroupHealthBusy, entry.Status)
	require.NotEqual(t, GroupConfidenceUnstable, entry.ConfidenceStatus)
}

func TestGroupStatusServiceMapsSuccessRatesToConfidencePanelCopy(t *testing.T) {
	db := setupGroupStatusServiceTest(t)
	insertGroupAbility(t, db, "excellent", "gpt-4o", 11, true)
	insertGroupAbility(t, db, "smooth", "gpt-4o", 12, true)
	insertGroupAbility(t, db, "stable", "gpt-4o", 13, true)
	insertGroupAbility(t, db, "unstable", "gpt-4o", 14, true)
	insertGroupAbility(t, db, "down", "gpt-4o", 15, true)
	insertGroupMetric(t, db, "excellent", 1000, 999, 1000, 1200)
	insertGroupMetric(t, db, "smooth", 1000, 991, 1000, 2600)
	insertGroupMetric(t, db, "stable", 1000, 972, 1000, 5200)
	insertGroupMetric(t, db, "unstable", 1000, 920, 1000, 1200)
	insertGroupMetric(t, db, "down", 1000, 305, 1000, 1200)

	result, err := GetUserGroupStatuses(GroupStatusRequest{
		UsableGroups: map[string]string{
			"excellent": "Excellent",
			"smooth":    "Smooth",
			"stable":    "Stable",
			"unstable":  "Unstable",
			"down":      "Down",
		},
		Hours: 6,
	})
	require.NoError(t, err)

	byGroup := groupStatusByName(result.Groups)
	require.Equal(t, GroupConfidenceExcellent, byGroup["excellent"].ConfidenceStatus)
	require.Equal(t, GroupRecommendationBest, byGroup["excellent"].RecommendationLevel)
	require.Equal(t, GroupExperienceLightning, byGroup["excellent"].ExperienceLabel)
	require.Equal(t, GroupConfidenceSmooth, byGroup["smooth"].ConfidenceStatus)
	require.Equal(t, GroupRecommendationRecommended, byGroup["smooth"].RecommendationLevel)
	require.Equal(t, GroupExperienceSmooth, byGroup["smooth"].ExperienceLabel)
	require.Equal(t, GroupConfidenceStable, byGroup["stable"].ConfidenceStatus)
	require.Equal(t, GroupRecommendationUsable, byGroup["stable"].RecommendationLevel)
	require.Equal(t, GroupExperienceNormal, byGroup["stable"].ExperienceLabel)
	require.Equal(t, GroupConfidenceUnstable, byGroup["unstable"].ConfidenceStatus)
	require.Equal(t, GroupRecommendationCaution, byGroup["unstable"].RecommendationLevel)
	require.Equal(t, GroupConfidenceUnavailable, byGroup["down"].ConfidenceStatus)
	require.Equal(t, GroupRecommendationUnavailable, byGroup["down"].RecommendationLevel)
}

func TestGroupStatusServiceReturnsUnknownForLowTrafficRoutableGroup(t *testing.T) {
	db := setupGroupStatusServiceTest(t)
	insertGroupAbility(t, db, "default", "gpt-4o", 1, true)
	insertGroupMetric(t, db, "default", 3, 3, 1000, 300)

	result, err := GetUserGroupStatuses(GroupStatusRequest{
		UsableGroups: map[string]string{"default": "Default"},
		Hours:        1,
	})
	require.NoError(t, err)

	require.Len(t, result.Groups, 1)
	require.Equal(t, GroupHealthUnknown, result.Groups[0].Status)
	require.Equal(t, GroupHealthConfidenceLow, result.Groups[0].Confidence)
	require.Equal(t, GroupConfidenceUnknown, result.Groups[0].ConfidenceStatus)
	require.Equal(t, GroupExperienceUnknown, result.Groups[0].ExperienceLabel)
}

func TestGroupStatusServiceFiltersToUserUsableGroups(t *testing.T) {
	db := setupGroupStatusServiceTest(t)
	insertGroupAbility(t, db, "default", "gpt-4o", 1, true)
	insertGroupAbility(t, db, "vip", "gpt-4o", 2, true)
	insertGroupMetric(t, db, "default", 50, 50, 1000, 300)
	insertGroupMetric(t, db, "vip", 50, 50, 1000, 300)

	result, err := GetUserGroupStatuses(GroupStatusRequest{
		UsableGroups: map[string]string{"vip": "VIP"},
		Hours:        24,
	})
	require.NoError(t, err)

	require.Equal(t, []string{"vip"}, groupStatusNames(result.Groups))
}

func TestGroupStatusServiceAutoUsesUserAutoGroupsAsRoutableFallback(t *testing.T) {
	db := setupGroupStatusServiceTest(t)
	insertGroupAbility(t, db, "default", "gpt-4o", 1, true)
	insertGroupAbility(t, db, "vip", "gpt-4o", 2, true)

	result, err := GetUserGroupStatuses(GroupStatusRequest{
		UsableGroups: map[string]string{
			"default": "Default",
			"vip":     "VIP",
			"auto":    "Auto",
		},
		Hours: 24,
	})
	require.NoError(t, err)

	byGroup := groupStatusByName(result.Groups)
	require.Equal(t, GroupHealthUnknown, byGroup["auto"].Status)
	require.Equal(t, GroupConfidenceUnknown, byGroup["auto"].ConfidenceStatus)
	require.Equal(t, int64(1), byGroup["auto"].AvailableModelCount)
}

func TestGroupStatusServiceAutoAggregatesUserAutoGroupMetrics(t *testing.T) {
	db := setupGroupStatusServiceTest(t)
	insertGroupAbility(t, db, "default", "gpt-4o", 1, true)
	insertGroupAbility(t, db, "vip", "gpt-4o", 2, true)
	insertGroupMetric(t, db, "default", 40, 40, 1000, 300)
	insertGroupMetric(t, db, "vip", 40, 40, 1200, 400)

	result, err := GetUserGroupStatuses(GroupStatusRequest{
		UsableGroups: map[string]string{
			"default": "Default",
			"vip":     "VIP",
			"auto":    "Auto",
		},
		Hours: 24,
	})
	require.NoError(t, err)

	auto := groupStatusByName(result.Groups)["auto"]
	require.Equal(t, GroupHealthOperational, auto.Status)
	require.Equal(t, GroupConfidenceExcellent, auto.ConfidenceStatus)
	require.Equal(t, int64(80), auto.RequestCount)
	require.Equal(t, 100.0, auto.SuccessRate)
}

func TestGroupStatusServiceTreatsDisabledChannelsAsNotRoutable(t *testing.T) {
	db := setupGroupStatusServiceTest(t)
	insertGroupAbilityWithChannelStatus(t, db, "default", "gpt-4o", 1, true, common.ChannelStatusAutoDisabled)
	insertGroupMetric(t, db, "default", 30, 30, 1000, 300)

	result, err := GetUserGroupStatuses(GroupStatusRequest{
		UsableGroups: map[string]string{"default": "Default"},
		Hours:        24,
	})
	require.NoError(t, err)

	entry := groupStatusByName(result.Groups)["default"]
	require.Equal(t, GroupHealthOutage, entry.Status)
	require.Equal(t, GroupConfidenceUnavailable, entry.ConfidenceStatus)
	require.Equal(t, int64(0), entry.AvailableModelCount)
}

func TestGroupStatusServiceReturnsUnknownExperienceWhenTtftSamplesAreInsufficient(t *testing.T) {
	db := setupGroupStatusServiceTest(t)
	insertGroupAbility(t, db, "default", "gpt-4o", 1, true)
	insertGroupMetricWithTtftCount(t, db, "default", 40, 40, 1000, 500, 10)

	result, err := GetUserGroupStatuses(GroupStatusRequest{
		UsableGroups: map[string]string{"default": "Default"},
		Hours:        24,
	})
	require.NoError(t, err)

	entry := groupStatusByName(result.Groups)["default"]
	require.Equal(t, GroupConfidenceExcellent, entry.ConfidenceStatus)
	require.Equal(t, GroupExperienceUnknown, entry.ExperienceLabel)
}

func TestGroupStatusServiceUsesRealtimeEventsForNowWindow(t *testing.T) {
	db := setupGroupStatusServiceTest(t)
	insertGroupAbility(t, db, "default", "gpt-4o", 1, true)
	perfmetrics.Record(perfmetrics.Sample{
		Model:     "gpt-4o",
		Group:     "default",
		LatencyMs: 1800,
		TtftMs:    600,
		HasTtft:   true,
		Success:   true,
	})

	result, err := GetUserGroupStatuses(GroupStatusRequest{
		UsableGroups: map[string]string{"default": "Default"},
		Window:       "now",
	})
	require.NoError(t, err)

	entry := groupStatusByName(result.Groups)["default"]
	require.Equal(t, "now", result.Window)
	require.Equal(t, 5, result.WindowMinutes)
	require.Equal(t, GroupHealthOperational, entry.Status)
	require.Equal(t, GroupConfidenceStable, entry.ConfidenceStatus)
	require.Equal(t, GroupRecommendationUsable, entry.RecommendationLevel)
	require.Equal(t, groupStatusMessageLiveSuccess, entry.DisplayMessage)
	require.Equal(t, int64(1), entry.RequestCount)
	require.Equal(t, 100.0, entry.SuccessRate)
	require.Len(t, entry.RecentEvents, 1)
	require.Equal(t, "success", entry.RecentEvents[0].Status)
}

func TestGroupStatusServiceShowsLiveFailureWhenRecentEventsFail(t *testing.T) {
	db := setupGroupStatusServiceTest(t)
	insertGroupAbility(t, db, "default", "gpt-4o", 1, true)
	for i := 0; i < 8; i++ {
		perfmetrics.Record(perfmetrics.Sample{
			Model:     "gpt-4o",
			Group:     "default",
			LatencyMs: 900,
			Success:   i < 2,
		})
	}

	result, err := GetUserGroupStatuses(GroupStatusRequest{
		UsableGroups: map[string]string{"default": "Default"},
		Window:       "now",
	})
	require.NoError(t, err)

	entry := groupStatusByName(result.Groups)["default"]
	require.Equal(t, GroupHealthOutage, entry.Status)
	require.Equal(t, GroupConfidenceUnavailable, entry.ConfidenceStatus)
	require.Equal(t, GroupRecommendationUnavailable, entry.RecommendationLevel)
	require.Equal(t, groupStatusMessageLiveFailure, entry.DisplayMessage)
	require.Equal(t, int64(8), entry.RequestCount)
	require.Equal(t, 25.0, entry.SuccessRate)
	require.Len(t, entry.RecentEvents, 8)
}

func TestGroupStatusServiceShowsRollingSignalsWithoutUsingOldEventsForNowStatus(t *testing.T) {
	db := setupGroupStatusServiceTest(t)
	insertGroupAbility(t, db, "default", "gpt-4o", 1, true)
	insertGroupMetricAt(t, db, "default", "gpt-4o", time.Now().Add(-2*time.Hour).Unix(), 80, 80, 1000, 300)
	for i := 0; i < 3; i++ {
		perfmetrics.Record(perfmetrics.Sample{
			Model:     "gpt-4o",
			Group:     "default",
			LatencyMs: 900,
			Success:   false,
		})
	}

	result, err := GetUserGroupStatuses(GroupStatusRequest{
		UsableGroups: map[string]string{"default": "Default"},
		Window:       "now",
	})
	require.NoError(t, err)

	entry := groupStatusByName(result.Groups)["default"]
	require.Equal(t, GroupHealthOutage, entry.Status)
	require.Equal(t, GroupConfidenceUnavailable, entry.ConfidenceStatus)
	require.Equal(t, int64(3), entry.RequestCount)
	require.Len(t, entry.RecentEvents, 3)
}

func TestGroupStatusServiceUsesRecentLogsForSignalStrip(t *testing.T) {
	db := setupGroupStatusServiceTest(t)
	insertGroupAbility(t, db, "default", "gpt-4o", 1, true)
	now := time.Now().Unix()
	for i := 0; i < 70; i++ {
		logType := model.LogTypeConsume
		if i%9 == 0 {
			logType = model.LogTypeError
		}
		require.NoError(t, db.Create(&model.Log{
			CreatedAt: now - int64(70-i),
			Type:      logType,
			Group:     "default",
			UseTime:   i,
		}).Error)
	}

	result, err := GetUserGroupStatuses(GroupStatusRequest{
		UsableGroups: map[string]string{"default": "Default"},
		Window:       "now",
	})
	require.NoError(t, err)

	entry := groupStatusByName(result.Groups)["default"]
	require.Equal(t, GroupHealthUnknown, entry.Status)
	require.Equal(t, int64(0), entry.RequestCount)
	require.Len(t, entry.RecentEvents, 60)
	require.Equal(t, now-60, entry.RecentEvents[0].Ts)
	require.Equal(t, now-1, entry.RecentEvents[59].Ts)
	require.Equal(t, "failure", entry.RecentEvents[8].Status)
}

func TestGroupStatusServiceKeepsRoutableNowWindowWaitingWithoutRecentEvents(t *testing.T) {
	db := setupGroupStatusServiceTest(t)
	insertGroupAbility(t, db, "default", "gpt-4o", 1, true)

	result, err := GetUserGroupStatuses(GroupStatusRequest{
		UsableGroups: map[string]string{"default": "Default"},
		Window:       "now",
	})
	require.NoError(t, err)

	entry := groupStatusByName(result.Groups)["default"]
	require.Equal(t, GroupHealthUnknown, entry.Status)
	require.Equal(t, GroupConfidenceUnknown, entry.ConfidenceStatus)
	require.Equal(t, groupStatusMessageLiveWaiting, entry.DisplayMessage)
	require.Empty(t, entry.RecentEvents)
}

func insertGroupAbility(t *testing.T, db *gorm.DB, group string, modelName string, channelID int, enabled bool) {
	t.Helper()

	insertGroupAbilityWithChannelStatus(t, db, group, modelName, channelID, enabled, common.ChannelStatusEnabled)
}

func insertGroupAbilityWithChannelStatus(t *testing.T, db *gorm.DB, group string, modelName string, channelID int, enabled bool, channelStatus int) {
	t.Helper()

	require.NoError(t, db.Create(&model.Channel{
		Id:     channelID,
		Name:   fmt.Sprintf("channel-%d", channelID),
		Key:    fmt.Sprintf("sk-test-%d", channelID),
		Status: channelStatus,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     group,
		Model:     modelName,
		ChannelId: channelID,
		Enabled:   enabled,
	}).Error)
}

func insertGroupMetric(t *testing.T, db *gorm.DB, group string, requestCount int64, successCount int64, avgLatencyMs int64, avgTtftMs int64) {
	t.Helper()
	insertGroupMetricAt(t, db, group, "gpt-4o", time.Now().Unix(), requestCount, successCount, avgLatencyMs, avgTtftMs)
}

func insertGroupMetricAt(t *testing.T, db *gorm.DB, group string, modelName string, bucketTs int64, requestCount int64, successCount int64, avgLatencyMs int64, avgTtftMs int64) {
	t.Helper()

	insertGroupMetricAtWithTtftCount(t, db, group, modelName, bucketTs, requestCount, successCount, avgLatencyMs, avgTtftMs, requestCount)
}

func insertGroupMetricWithTtftCount(t *testing.T, db *gorm.DB, group string, requestCount int64, successCount int64, avgLatencyMs int64, avgTtftMs int64, ttftCount int64) {
	t.Helper()

	insertGroupMetricAtWithTtftCount(t, db, group, "gpt-4o", time.Now().Unix(), requestCount, successCount, avgLatencyMs, avgTtftMs, ttftCount)
}

func insertGroupMetricAtWithTtftCount(t *testing.T, db *gorm.DB, group string, modelName string, bucketTs int64, requestCount int64, successCount int64, avgLatencyMs int64, avgTtftMs int64, ttftCount int64) {
	t.Helper()

	require.NoError(t, db.Create(&model.PerfMetric{
		ModelName:      modelName,
		Group:          group,
		BucketTs:       bucketTs,
		RequestCount:   requestCount,
		SuccessCount:   successCount,
		TotalLatencyMs: requestCount * avgLatencyMs,
		TtftSumMs:      ttftCount * avgTtftMs,
		TtftCount:      ttftCount,
	}).Error)
}

func groupStatusByName(entries []GroupStatusEntry) map[string]GroupStatusEntry {
	byGroup := make(map[string]GroupStatusEntry, len(entries))
	for _, entry := range entries {
		byGroup[entry.Group] = entry
	}
	return byGroup
}

func groupStatusNames(entries []GroupStatusEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Group)
	}
	return names
}
