package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	jsadaptor "github.com/QuantumNous/new-api/relay/channel/task/jsplugin"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This uses the real JS parser, durable task boundary and wallet settlement.
// A synchronous completion must never become a pollable SUBMITTED task.
func TestPluginImmediateTaskPersistsUsageAndState(t *testing.T) {
	for _, tc := range []struct {
		name, status     string
		units, wantQuota int
		discard          bool
	}{
		{"success", "SUCCESS", 2, 10000, false},
		{"zero", "SUCCESS", 0, 0, false},
		{"failure", "FAILURE", 2, 0, false},
		{"inline", "SUCCESS", 2, 10000, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTaskReliabilityDB(t)
			require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Log{}, &model.UserSubscription{}))
			if service.GetHttpClient() == nil {
				service.InitHttpClient()
			}
			previousRedis, previousMemory := common.RedisEnabled, common.MemoryCacheEnabled
			common.RedisEnabled, common.MemoryCacheEnabled = false, false
			t.Cleanup(func() { common.RedisEnabled, common.MemoryCacheEnabled = previousRedis, previousMemory })
			require.NoError(t, db.Create(&model.User{Id: 42, Username: "plugin-billing", Quota: 100000}).Error)
			recorder, c, info := newTaskReliabilityContext(t)
			info.PriceData = types.PriceData{Quota: 20000, QuotaToPreConsume: 20000, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}}
			info.IsPlayground = true
			expr := `tier("usage", u("units") * 0.01)`
			info.TieredBillingSnapshot = &billingexpr.BillingSnapshot{BillingMode: "tiered_expr", ExprString: expr, ExprHash: billingexpr.ExprHashString(expr), TaskUsageBilling: true, QuotaPerUnit: 500000, GroupRatio: 1, UsageFacts: map[string]any{"units": 4.0}}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				payload, err := common.Marshal(map[string]any{"status": tc.status, "usage": map[string]any{"units": tc.units}, "id": "upstream-private"})
				require.NoError(t, err)
				_, err = w.Write(payload)
				require.NoError(t, err)
			}))
			defer server.Close()
			info.ChannelBaseUrl = server.URL
			plugin, err := jsplugin.CompilePlugin(`
export const meta={apiVersion:1,key:"immediate-task",name:"Immediate",version:"1.0.0",author:{name:"Test"},models:["test-video-model"],fetchMode:"per_task",usageSchema:{units:{type:"number",unit:"count"}}};
export function buildSubmitRequest(ctx){return {url:ctx.baseUrl+"/submit",body:{model:ctx.upstreamModel}};}
export function parseSubmitResponse(ctx,r){return {taskId:r.body.id,taskData:r.body,state:{cursor:"resume"},immediate:{status:r.body.status,reason:"provider failure"}};}
export function extractUsage(){return {units:4};}
export function extractUsageOnComplete(ctx,result,body){return body.usage;}
export function buildQueryRequest(){throw new Error("terminal task must not poll");}
export function parseTaskResult(){throw new Error("terminal task must not poll");}
`, jsplugin.Options{})
			require.NoError(t, err)
			c.Set(jsplugin.ContextKeyPinnedPlugin, jsplugin.PinnedPlugin{Plugin: plugin})
			if tc.discard {
				retain := false
				c.Set(jsplugin.ContextKeyPinnedRoute, jsplugin.PinnedRoute{Plugin: plugin, Route: jsplugin.Route{RetainResult: &retain}})
			}
			c.Set("task_request", map[string]any{"model": "test-video-model"})
			adaptor := jsadaptor.New(plugin)
			adaptor.Init(info)
			require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
			result, taskErr := submitPreparedTask(c, info, adaptor, "immediate-task", nil)
			require.Nil(t, taskErr)
			require.NotNil(t, result)
			require.NoError(t, service.SettleBilling(c, info, result.Quota))
			assert.Empty(t, recorder.Body.String())
			assert.Equal(t, tc.wantQuota, result.Quota)
			var task model.Task
			require.NoError(t, db.First(&task, result.Task.ID).Error)
			assert.EqualValues(t, tc.status, task.Status)
			assert.NotZero(t, task.FinishTime)
			assert.Equal(t, tc.wantQuota, task.Quota)
			require.NotNil(t, task.PrivateData.Execution)
			require.NotNil(t, task.PrivateData.Execution.TaskPlugin)
			assert.Equal(t, "immediate-task", task.PrivateData.Execution.TaskPlugin.Key)
			require.NotNil(t, task.PrivateData.BillingContext.TieredSnapshot)
			if tc.status == "SUCCESS" {
				assert.Equal(t, float64(tc.units), task.PrivateData.BillingContext.TieredSnapshot.UsageFacts["units"])
			}
			if tc.discard {
				assert.True(t, task.PrivateData.ResultDiscarded)
				assert.Empty(t, task.Data)
				assert.Empty(t, task.PrivateData.PluginState)
				assert.NotEmpty(t, result.Task.Data, "inline response keeps its one-shot result")
			} else {
				assert.JSONEq(t, `{"cursor":"resume"}`, string(task.PrivateData.PluginState))
			}
			var user model.User
			require.NoError(t, db.First(&user, 42).Error)
			assert.EqualValues(t, 100000-tc.wantQuota, user.Quota)
		})
	}
}
