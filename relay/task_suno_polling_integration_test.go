package relay

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSunoPollingSettlesSharedProviderIDPerChannel(t *testing.T) {
	db := setupTaskReliabilityDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&model.User{}))
	originalFactory, originalCache, originalLimit := service.GetTaskAdaptorFunc, common.MemoryCacheEnabled, constant.TaskQueryLimit
	originalRedis := common.RedisEnabled
	common.RedisEnabled = false
	service.GetTaskAdaptorFunc = func(platform constant.TaskPlatform) service.TaskPollingAdaptor { return GetTaskAdaptor(platform) }
	common.MemoryCacheEnabled, constant.TaskQueryLimit = false, 10
	t.Cleanup(func() {
		common.RedisEnabled = originalRedis
		service.GetTaskAdaptorFunc, common.MemoryCacheEnabled, constant.TaskQueryLimit = originalFactory, originalCache, originalLimit
	})
	service.InitHttpClient()
	require.NotNil(t, GetTaskAdaptor(constant.TaskPlatformSuno))

	const expression = `tier("music", u("clips") * 0.75)`
	const reservation = 100
	ctx := t.Context()
	var calls [2]atomic.Int32
	scenarios := []struct {
		key       string
		body      string
		status    model.TaskStatus
		balance   int64
		charge    int
		operation string
		delta     int
	}{
		{"accepted-a", `{"code":"success","data":[{"task_id":"shared","status":"SUCCESS","data":[{"id":"clip-1","audio_url":"https://example.test/1"},{"id":"clip-2","audio_url":"https://example.test/2"}]}]}`, model.TaskStatusSuccess, 850, 150, model.TaskBillingAuditOperationSettlement, 50},
		{"accepted-b", `{"code":"success","data":[{"task_id":"shared","status":"FAILURE","fail_reason":"provider failure"}]}`, model.TaskStatusFailure, 1000, 0, model.TaskBillingAuditOperationRefund, 100},
	}
	for i, scenario := range scenarios {
		provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls[i].Add(1)
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/suno/fetch", r.URL.Path)
			assert.Equal(t, "Bearer "+scenario.key, r.Header.Get("Authorization"))
			input, readErr := io.ReadAll(r.Body)
			assert.NoError(t, readErr)
			assert.JSONEq(t, `{"ids":["shared"]}`, string(input))
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, scenario.body)
		}))
		t.Cleanup(provider.Close)

		id := i + 1
		channel := model.Channel{
			Id: id, Type: constant.ChannelTypeSunoAPI, BaseURL: &provider.URL,
			Key: "unused\n" + scenario.key, ChannelInfo: model.ChannelInfo{IsMultiKey: true},
		}
		user := model.User{Id: id, Username: fmt.Sprintf("listener-%d", id), AffCode: fmt.Sprint(id), Quota: 1000, Status: common.UserStatusEnabled}
		require.NoError(t, db.Create(&channel).Error)
		require.NoError(t, db.Create(&user).Error)
		task := model.Task{
			TaskID: fmt.Sprintf("local-%d", id), UserId: id, ChannelId: id, Platform: constant.TaskPlatformSuno,
			Status: model.TaskStatusNotStart, SubmitTime: time.Now().Unix(),
			PrivateData: model.TaskPrivateData{BillingContext: &model.TaskBillingContext{}},
		}
		require.NoError(t, db.Create(&task).Error)
		reserved, err := model.ReserveTaskBilling(ctx, task.ID, model.TaskBillingReservationRequest{Source: model.TaskBillingSourceWallet, Quota: reservation, SkipToken: true})
		require.NoError(t, err)
		require.True(t, reserved.Applied)
		_, claimed, err := model.ClaimTaskSubmission(ctx, task.ID, &model.TaskSubmissionAttempt{
			Platform: constant.TaskPlatformSuno, ChannelID: id, ChannelType: channel.Type,
			Credential: scenario.key, OriginModelName: "suno_music",
		})
		require.NoError(t, err)
		require.True(t, claimed)
		_, accepted, err := model.PersistTaskSubmissionAcceptance(ctx, task.ID, model.TaskSubmissionAcceptance{
			UpstreamTaskID: "shared", ChannelID: id, CredentialDigest: model.CredentialDigest(scenario.key),
			Status: model.TaskStatusSubmitted, Progress: "0%", TargetQuota: reservation,
			TieredSnapshot: &billingexpr.BillingSnapshot{
				ModelName: "suno_music", ExprString: expression, ExprHash: billingexpr.ExprHashString(expression),
				GroupRatio: 1, QuotaPerUnit: 100, ExprVersion: 1, TaskUsageBilling: true,
			},
		})
		require.NoError(t, err)
		require.True(t, accepted)
		require.NoError(t, db.First(&user, id).Error)
		require.EqualValues(t, 1000-reservation, user.Quota)
	}

	summary := service.RunTaskPollingOnce(ctx, nil)
	require.Equal(t, 2, summary.UnfinishedTasks)
	assert.Zero(t, service.RunTaskPollingOnce(ctx, nil).UnfinishedTasks)
	for i, scenario := range scenarios {
		id := i + 1
		assert.EqualValues(t, 1, calls[i].Load())
		var task model.Task
		require.NoError(t, db.Where("user_id = ? AND channel_id = ?", id, id).First(&task).Error)
		assert.Equal(t, scenario.status, task.Status)
		assert.Equal(t, scenario.charge, task.Quota)
		var user model.User
		require.NoError(t, db.First(&user, id).Error)
		assert.Equal(t, scenario.balance, user.Quota)
		var events []model.KKAIOutboxEvent
		require.NoError(t, db.Where("topic = ? AND aggregate_id = ?", model.KKAIOutboxTopicTaskBillingAudit, task.TaskID).Find(&events).Error)
		require.Len(t, events, 1)
		var audit model.TaskBillingAuditPayload
		require.NoError(t, common.UnmarshalJsonStr(events[0].Payload, &audit))
		assert.Equal(t, task.ID, audit.TaskID)
		assert.Equal(t, scenario.operation, audit.Operation)
		assert.Equal(t, scenario.delta, audit.Quota)
		assert.Equal(t, reservation, audit.PreviousQuota)
		assert.Equal(t, scenario.charge, audit.CurrentQuota)
	}
}
