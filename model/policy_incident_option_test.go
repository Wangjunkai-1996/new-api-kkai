package model

import (
	"bytes"
	"strconv"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetPolicyOptionTest(t *testing.T) operation_setting.PolicyIncidentSetting {
	t.Helper()
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	require.NoError(t, DB.Exec("DELETE FROM options").Error)
	common.OptionMapRWMutex.Lock()
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	original := operation_setting.GetPolicyIncidentSetting()
	t.Cleanup(func() { require.NoError(t, operation_setting.PublishPolicyIncidentSetting(original)) })
	require.NoError(t, operation_setting.PublishPolicyIncidentSetting(operation_setting.DefaultPolicyIncidentSetting()))
	return original
}

func TestUpdateOptionRejectsRemovedEvidenceOptionsBeforePersistence(t *testing.T) {
	resetPolicyOptionTest(t)
	for _, key := range []string{
		"policy_incident_setting.evidence_enabled",
		"policy_incident_setting.evidence_retention_hours",
		"policy_incident_setting.evidence_max_total_bytes",
		"policy_incident_setting.evidence_max_file_bytes",
	} {
		require.Error(t, UpdateOption(key, "true"))
		var count int64
		require.NoError(t, DB.Model(&Option{}).Where("key = ?", key).Count(&count).Error)
		assert.Zero(t, count)
	}
}

func TestUpdateOptionsBulkPublishesSafetySettingsAtomically(t *testing.T) {
	resetPolicyOptionTest(t)
	require.NoError(t, UpdateOptionsBulk(map[string]string{
		"policy_incident_setting.disable_client_token_persistently":   "true",
		"policy_incident_setting.isolate_upstream_on_policy_incident": "true",
	}))
	snapshot := operation_setting.GetPolicyIncidentSetting()
	assert.True(t, snapshot.DisableClientTokenPersistently)
	assert.True(t, snapshot.IsolateUpstreamOnPolicyIncident)
}

func TestConcurrentPolicyOptionUpdatesPreserveWholePersistedGroup(t *testing.T) {
	resetPolicyOptionTest(t)
	start := make(chan struct{})
	errorsByUpdate := make(chan error, 2)
	var waitGroup sync.WaitGroup
	for key, value := range map[string]string{
		"policy_incident_setting.disable_client_token_persistently":   "true",
		"policy_incident_setting.isolate_upstream_on_policy_incident": "true",
	} {
		waitGroup.Add(1)
		go func(key, value string) {
			defer waitGroup.Done()
			<-start
			errorsByUpdate <- UpdateOption(key, value)
		}(key, value)
	}
	close(start)
	waitGroup.Wait()
	close(errorsByUpdate)
	for err := range errorsByUpdate {
		require.NoError(t, err)
	}
	snapshot := operation_setting.GetPolicyIncidentSetting()
	assert.True(t, snapshot.DisableClientTokenPersistently)
	assert.True(t, snapshot.IsolateUpstreamOnPolicyIncident)
}

func TestUpdateOptionsBulkDoesNotPublishWhenDatabaseRollsBack(t *testing.T) {
	original := resetPolicyOptionTest(t)
	require.NoError(t, DB.Exec(`
		CREATE TRIGGER reject_policy_option_update
		BEFORE INSERT ON options
		BEGIN
			SELECT RAISE(FAIL, 'forced option failure');
		END
	`).Error)
	t.Cleanup(func() { DB.Exec("DROP TRIGGER IF EXISTS reject_policy_option_update") })
	err := UpdateOptionsBulk(map[string]string{
		"policy_incident_setting.disable_client_token_persistently": "true",
	})
	require.Error(t, err)
	assert.Equal(t, operation_setting.DefaultPolicyIncidentSetting(), operation_setting.GetPolicyIncidentSetting())
	assert.NotEqual(t, original.DisableClientTokenPersistently, true)
}

func TestLoadOptionsPublishesSafetyGroupAndIgnoresRowOrder(t *testing.T) {
	resetPolicyOptionTest(t)
	require.NoError(t, DB.Create([]Option{
		{Key: "policy_incident_setting.isolate_upstream_on_policy_incident", Value: "true"},
		{Key: "policy_incident_setting.disable_client_token_persistently", Value: "true"},
	}).Error)
	loadOptionsFromDatabase()
	assert.Equal(t, operation_setting.PolicyIncidentSetting{
		DisableClientTokenPersistently:  true,
		IsolateUpstreamOnPolicyIncident: true,
	}, operation_setting.GetPolicyIncidentSetting())
}

func TestPolicySettingsRemainVisibleThroughOptionMap(t *testing.T) {
	resetPolicyOptionTest(t)
	require.NoError(t, UpdateOption("policy_incident_setting.disable_client_token_persistently", "true"))
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	assert.Equal(t, "true", common.OptionMap["policy_incident_setting.disable_client_token_persistently"])
	assert.Equal(t, "false", common.OptionMap["policy_incident_setting.isolate_upstream_on_policy_incident"])
}

func TestLoadOptionsFailsClosedWithoutLoggingInvalidValue(t *testing.T) {
	resetPolicyOptionTest(t)
	require.NoError(t, DB.Create(&Option{
		Key: "policy_incident_setting.disable_client_token_persistently", Value: "provider secret invalid",
	}).Error)
	var output bytes.Buffer
	common.LogWriterMu.Lock()
	originalWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &output
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = originalWriter
		common.LogWriterMu.Unlock()
	})
	loadOptionsFromDatabase()
	assert.Equal(t, operation_setting.DefaultPolicyIncidentSetting(), operation_setting.GetPolicyIncidentSetting())
	assert.Contains(t, output.String(), "policy_incident_config_load_failed code=invalid_config")
	assert.NotContains(t, output.String(), "provider secret")
}

func TestLoadOptionsStillLoadsOtherOptionsWhenPolicyGroupInvalid(t *testing.T) {
	resetPolicyOptionTest(t)
	const retryTimes = 17
	originalRetryTimes := common.RetryTimes
	t.Cleanup(func() { common.RetryTimes = originalRetryTimes })
	require.NoError(t, DB.Create([]Option{
		{Key: "policy_incident_setting.isolate_upstream_on_policy_incident", Value: "invalid"},
		{Key: "RetryTimes", Value: strconv.Itoa(retryTimes)},
	}).Error)
	loadOptionsFromDatabase()
	assert.Equal(t, operation_setting.DefaultPolicyIncidentSetting(), operation_setting.GetPolicyIncidentSetting())
	assert.Equal(t, retryTimes, common.RetryTimes)
}
