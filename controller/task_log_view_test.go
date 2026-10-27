package controller

import (
	"encoding/base64"
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setTaskLogCreditEpoch(t *testing.T, marker string) {
	t.Helper()
	common.OptionMapRWMutex.Lock()
	previous := common.OptionMap
	common.OptionMap = map[string]string{common.CreditEpochOption: marker}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previous
		common.OptionMapRWMutex.Unlock()
	})
}

func taskLogCreditEpochMarker(t *testing.T) string {
	t.Helper()
	marker, err := common.Marshal(common.CreditEpochCutover{
		Version: 1, MigrationID: "task-billing-display", SourceEpoch: common.CreditEpochLegacy,
		TargetEpoch: common.CreditEpochUSD, Numerator: 3, Denominator: 40,
		PlanHash: "plan", PricingPlanHash: "pricing", AppliedAt: 1,
		LegacyTopUpPolicy: "hold", LegacyMaxIDs: map[string]int64{"tasks": 10},
	})
	require.NoError(t, err)
	return string(marker)
}

func TestTaskLogDTOBillingProjectsPublicSnapshot(t *testing.T) {
	setTaskLogCreditEpoch(t, taskLogCreditEpochMarker(t))
	task := &model.Task{
		ID: 11, TaskID: "task_billed", Quota: 100,
		PrivateData: model.TaskPrivateData{
			Key: "secret-key", UpstreamTaskID: "secret-upstream-id", NodeName: "secret-node",
			BillingState: model.TaskBillingStateCompleted, BillingSource: model.TaskBillingSourceWallet,
			BillingContext: &model.TaskBillingContext{
				ModelPrice: 0.2, GroupRatio: 0.8, PerCallBilling: true,
				OtherRatios: map[string]float64{"seconds": 12, "size": 1.5, "unexpected_ratio": 2},
			},
			AccountingContext: &model.TaskAccountingContext{
				HasUserGroupRatio: true, UserGroupRatio: 0.7, TokenName: "secret-token",
			},
		},
	}
	expected := map[string]any{
		"state": "completed", "billing_source": "wallet", "pricing_epoch": common.CreditEpochUSD,
		"model_price": 0.2, "group_ratio": 0.8, "user_group_ratio": 0.7,
		"billing_unit": "second", "seconds": float64(12), "size": 1.5,
	}
	for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser} {
		view := tasksToDto([]*model.Task{task}, false, role)[0]
		assert.Equal(t, expected, taskBillingFields(t, view.Billing))
		assert.Equal(t, 100, view.Quota)
		encoded, err := common.Marshal(view.Billing)
		require.NoError(t, err)
		assert.NotContains(t, string(encoded), "secret")
	}
}

func TestTaskLogDTOBillingOmitsUnconfirmedAndLegacyPrices(t *testing.T) {
	for _, test := range []struct {
		name   string
		marker string
		id     int64
		epoch  string
	}{
		{name: "legacy", marker: taskLogCreditEpochMarker(t), id: 10, epoch: common.CreditEpochLegacy},
		{name: "missing marker", id: 11},
		{name: "invalid marker", marker: "{}", id: 11},
		{name: "missing identity", marker: taskLogCreditEpochMarker(t)},
	} {
		t.Run(test.name, func(t *testing.T) {
			setTaskLogCreditEpoch(t, test.marker)
			task := &model.Task{ID: test.id, Quota: 100, PrivateData: model.TaskPrivateData{
				BillingState: model.TaskBillingStateRefunded, BillingSource: model.TaskBillingSourceWallet,
				BillingContext: &model.TaskBillingContext{ModelPrice: 2, GroupRatio: 1},
			}}
			view := tasksToDto([]*model.Task{task}, false, common.RoleCommonUser)[0]
			expected := map[string]any{"state": "refunded", "billing_source": "wallet"}
			if test.epoch != "" {
				expected["pricing_epoch"] = test.epoch
				assert.Equal(t, 8, view.Quota, "legacy total uses the existing historical quota conversion")
			}
			assert.Equal(t, expected, taskBillingFields(t, view.Billing))
		})
	}
}

func TestTaskLogDTOBillingPreservesFrozenTieredAndMembershipTerms(t *testing.T) {
	setTaskLogCreditEpoch(t, taskLogCreditEpochMarker(t))
	task := &model.Task{ID: 11, PrivateData: model.TaskPrivateData{
		BillingState: model.TaskBillingStateAccepted, BillingSource: model.MembershipSource,
		MembershipBillingRequestID: "secret-membership-request",
		MembershipAssessedQuota:    999999, MembershipExcessQuota: 888888,
		MembershipPricing: &types.MembershipPricing{
			TierName: "Gold", TierStyle: "gold", Discount: 0.8, WalletRatio: 1, BaseRatio: 1,
			Price: types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.8}},
		},
		BillingContext: &model.TaskBillingContext{GroupRatio: 0.8, TieredSnapshot: &billingexpr.BillingSnapshot{
			ExprString: `tier("video", u("seconds") * 2)`, DisplayExprString: `tier("video", u("seconds") * 3)`,
			EstimatedTier: "video", UsageFacts: map[string]any{"seconds": float64(12)},
		}},
	}}
	view := tasksToDto([]*model.Task{task}, false, common.RoleCommonUser)[0]
	assert.Equal(t, "accepted", taskBillingFields(t, view.Billing)["state"])
	assert.Equal(t, "membership", taskBillingFields(t, view.Billing)["billing_source"])
	assert.Equal(t, "Gold", taskBillingFields(t, view.Billing)["membership_tier_name"])
	assert.Equal(t, 0.8, taskBillingFields(t, view.Billing)["membership_discount"])
	assert.Equal(t, "tiered_expr", taskBillingFields(t, view.Billing)["billing_mode"])
	assert.Equal(t, "video", taskBillingFields(t, view.Billing)["matched_tier"])
	assert.Equal(t, map[string]any{"seconds": float64(12)}, taskBillingFields(t, view.Billing)["usage_facts"])
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte(`tier("video", u("seconds") * 3)`)), taskBillingFields(t, view.Billing)["expr_b64"])
	encoded, err := common.Marshal(view.Billing)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), base64.StdEncoding.EncodeToString([]byte(`tier("video", u("seconds") * 2)`)))
	assert.NotContains(t, string(encoded), "secret")
	assert.NotContains(t, string(encoded), "quota", "a snapshot cannot invent reserved, final, refunded, or saved amounts")

	task.TenantID = model.PlatformTenantID + 1
	view = tasksToDto([]*model.Task{task}, false, common.RoleCommonUser)[0]
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte(`tier("video", u("seconds") * 2)`)), taskBillingFields(t, view.Billing)["expr_b64"])
}

func TestTaskLogDTOBillingRejectsPrivateOrInvalidFactsAndPreservesKnownZero(t *testing.T) {
	setTaskLogCreditEpoch(t, taskLogCreditEpochMarker(t))
	task := &model.Task{ID: 11, PrivateData: model.TaskPrivateData{
		AccountingContext: &model.TaskAccountingContext{HasUserGroupRatio: true, UserGroupRatio: 0},
		BillingContext: &model.TaskBillingContext{
			ModelPrice: math.Inf(1), GroupRatio: math.NaN(),
			TieredSnapshot: &billingexpr.BillingSnapshot{
				ExprString: `tier("video", u("seconds") + u("image_count") + u("secret"))`,
				UsageFacts: map[string]any{"seconds": float64(0), "image_count": math.Inf(1), "secret": "bearer-token", "duration": float64(9), "headers": map[string]any{"Authorization": "secret-key"}},
			},
		},
	}}
	fields := taskBillingFields(t, tasksToDto([]*model.Task{task}, false, common.RoleCommonUser)[0].Billing)
	assert.Equal(t, float64(0), fields["user_group_ratio"])
	assert.Equal(t, map[string]any{"seconds": float64(0)}, fields["usage_facts"])
	assert.NotContains(t, fields, "model_price")
	assert.NotContains(t, fields, "group_ratio")
	assert.NotContains(t, fields, "image_count")
	encoded, err := common.Marshal(fields)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "bearer-token")
	assert.NotContains(t, string(encoded), "secret-key")
}

func TestTaskLogDTODoesNotInferBillingFromGenerationStatus(t *testing.T) {
	setTaskLogCreditEpoch(t, "")
	for _, status := range []model.TaskStatus{model.TaskStatusSuccess, model.TaskStatusFailure} {
		task := &model.Task{Status: status, PrivateData: model.TaskPrivateData{
			BillingState: "unknown-future-state", BillingSource: "unknown-future-source",
		}}
		view := tasksToDto([]*model.Task{task}, false, common.RoleCommonUser)[0]
		assert.Empty(t, view.Billing)
		encoded, err := common.Marshal(view)
		require.NoError(t, err)
		assert.NotContains(t, string(encoded), `"billing"`)
	}
}

func TestTaskLogDTOSeparatesUserAdminAndRootDetails(t *testing.T) {
	task := &model.Task{
		TaskID:   "task_public",
		Platform: "document-parser",
		PrivateData: model.TaskPrivateData{
			Key:            "channel-secret-canary",
			UpstreamTaskID: "upstream-private",
			NodeName:       "node-a",
			Execution: &model.TaskExecutionSnapshot{
				RequestID:   "request-public",
				RequestPath: "/v1/documents",
				TaskPlugin: &model.TaskPluginSnapshot{
					Key:     "document-parser",
					Name:    "Document Parser",
					Version: "1.2.3",
					Author: &model.TaskPluginAuthorSnapshot{
						Name: "Community Author",
						URL:  "https://plugins.example/author",
					},
					APIVersion: 1,
					Generation: 42,
				},
			},
		},
	}

	userView := tasksToDto([]*model.Task{task}, false, common.RoleCommonUser)[0]
	assert.Nil(t, userView.AdminInfo)
	assert.Nil(t, userView.RootInfo)

	adminView := tasksToDto([]*model.Task{task}, false, common.RoleAdminUser)[0]
	require.NotNil(t, adminView.AdminInfo)
	require.NotNil(t, adminView.AdminInfo.TaskPlugin)
	assert.Equal(t, "document-parser", adminView.AdminInfo.TaskPlugin.Key)
	assert.Equal(t, "Document Parser", adminView.AdminInfo.TaskPlugin.Name)
	assert.Equal(t, "1.2.3", adminView.AdminInfo.TaskPlugin.Version)
	require.NotNil(t, adminView.AdminInfo.TaskPlugin.Author)
	assert.Equal(t, "Community Author", adminView.AdminInfo.TaskPlugin.Author.Name)
	assert.Equal(t, "https://plugins.example/author", adminView.AdminInfo.TaskPlugin.Author.URL)
	assert.Equal(t, "request-public", adminView.AdminInfo.RequestID)
	assert.Equal(t, "/v1/documents", adminView.AdminInfo.RequestPath)
	assert.Nil(t, adminView.RootInfo)

	rootView := tasksToDto([]*model.Task{task}, false, common.RoleRootUser)[0]
	require.NotNil(t, rootView.AdminInfo)
	require.NotNil(t, rootView.RootInfo)
	require.NotNil(t, rootView.RootInfo.TaskPlugin)
	assert.Equal(t, 1, rootView.RootInfo.TaskPlugin.APIVersion)
	assert.Equal(t, uint64(42), rootView.RootInfo.TaskPlugin.Generation)
	assert.Equal(t, "upstream-private", rootView.RootInfo.UpstreamTaskID)
	assert.Equal(t, "node-a", rootView.RootInfo.NodeName)

	adminJSON, err := common.Marshal(adminView)
	require.NoError(t, err)
	assert.NotContains(t, string(adminJSON), "channel-secret-canary")
	assert.NotContains(t, string(adminJSON), "upstream-private")

	rootJSON, err := common.Marshal(rootView)
	require.NoError(t, err)
	assert.NotContains(t, string(rootJSON), "channel-secret-canary")
	assert.Contains(t, string(rootJSON), "upstream-private")
}

func TestTaskLogDTODoesNotInventHistoricalPluginProvenance(t *testing.T) {
	task := &model.Task{
		TaskID:   "task_without_snapshot",
		Platform: "document-parser",
	}

	adminView := tasksToDto([]*model.Task{task}, false, common.RoleAdminUser)[0]

	assert.Nil(t, adminView.AdminInfo)
	assert.Nil(t, adminView.RootInfo)
}

func TestTaskLogDTOReplacesLegacyVideoURLWithAvailabilityFlag(t *testing.T) {
	task := &model.Task{
		TaskID:     "task_legacy_video",
		Platform:   "jimeng",
		Action:     constant.TaskActionTextToVideo,
		Status:     model.TaskStatusSuccess,
		FailReason: "https://private-upstream.invalid/video.mp4?signature=secret",
	}

	view := tasksToDto([]*model.Task{task}, false, common.RoleCommonUser)[0]
	assert.True(t, view.LegacyVideoAvailable)
	assert.Empty(t, view.ResultURL)
	assert.Empty(t, view.FailReason)
	encoded, err := common.Marshal(view)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "private-upstream.invalid")
	assert.NotContains(t, string(encoded), "result_url")
	assert.Contains(t, string(encoded), "legacy_video_available")
}

func TestTaskLogDTOKeepsFailureReasonAndDoesNotMarkPluginTaskLegacy(t *testing.T) {
	failed := &model.Task{
		TaskID:     "task_failed",
		Platform:   "jimeng",
		Action:     constant.TaskActionTextToVideo,
		Status:     model.TaskStatusFailure,
		FailReason: "provider rejected the request",
	}
	failedView := tasksToDto([]*model.Task{failed}, false, common.RoleCommonUser)[0]
	assert.Equal(t, "provider rejected the request", failedView.FailReason)
	assert.False(t, failedView.LegacyVideoAvailable)

	pluginTask := &model.Task{
		TaskID:     "task_plugin_video",
		Platform:   "community-video",
		Action:     constant.TaskActionTextToVideo,
		Status:     model.TaskStatusSuccess,
		FailReason: "https://stale-upstream.invalid/plugin-video.mp4",
		PrivateData: model.TaskPrivateData{
			ResultURL: "https://private-upstream.invalid/plugin-video.mp4",
			Execution: &model.TaskExecutionSnapshot{
				TaskPlugin: &model.TaskPluginSnapshot{Key: "community-video"},
			},
		},
	}
	pluginView := tasksToDto([]*model.Task{pluginTask}, false, common.RoleCommonUser)[0]
	assert.False(t, pluginView.LegacyVideoAvailable)
	assert.Empty(t, pluginView.ResultURL)
	assert.Empty(t, pluginView.FailReason)
}

func taskBillingFields(t *testing.T, billing *dto.TaskBilling) map[string]any {
	t.Helper()
	encoded, err := common.Marshal(billing)
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, common.Unmarshal(encoded, &fields))
	return fields
}
