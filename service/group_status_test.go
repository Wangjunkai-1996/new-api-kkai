package service

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
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

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.PerfMetric{}))

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
	require.Equal(t, GroupHealthDegraded, byGroup["vip"].Status)
	require.Equal(t, GroupHealthBusy, byGroup["slow"].Status)
	require.Equal(t, GroupHealthOutage, byGroup["down"].Status)
	require.Equal(t, GroupHealthOutage, byGroup["empty"].Status)
	require.Equal(t, int64(0), byGroup["empty"].AvailableModelCount)
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
	require.Equal(t, int64(0), entry.AvailableModelCount)
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

	require.NoError(t, db.Create(&model.PerfMetric{
		ModelName:      modelName,
		Group:          group,
		BucketTs:       bucketTs,
		RequestCount:   requestCount,
		SuccessCount:   successCount,
		TotalLatencyMs: requestCount * avgLatencyMs,
		TtftSumMs:      requestCount * avgTtftMs,
		TtftCount:      requestCount,
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
