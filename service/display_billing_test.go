package service

import (
	"encoding/base64"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/config"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCacheReadDisplayBillingKeepsActualAccounting(t *testing.T) {
	const actual = `tier("base", p * 10 + cr * 2)`
	const display = `tier("base", p * 10 + cr * 0)`
	const startingQuota = 2_000_000
	truncate(t)
	user := model.User{Username: "cache-display-user", Quota: startingQuota, Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(&user).Error)
	token := model.Token{UserId: user.Id, Key: "cache-display-token", Name: "display-test", RemainQuota: startingQuota, Status: common.TokenStatusEnabled}
	require.NoError(t, model.DB.Create(&token).Error)
	channel := model.Channel{Name: "cache-display", Key: "unused", Status: common.ChannelStatusEnabled}
	require.NoError(t, model.DB.Create(&channel).Error)

	reservation := common.QuotaRound(10000 / 1_000_000.0 * common.QuotaPerUnit)
	want := common.QuotaRound(2000 / 1_000_000.0 * common.QuotaPerUnit)
	snapshot := &billingexpr.BillingSnapshot{
		BillingMode: "tiered_expr", ExprString: actual, DisplayExprString: display,
		ExprHash: billingexpr.ExprHashString(actual), GroupRatio: 1, QuotaPerUnit: common.QuotaPerUnit,
		EstimatedQuotaAfterGroup: reservation, EstimatedTier: "base",
	}
	info := &relaycommon.RelayInfo{
		UserId: user.Id, TokenId: token.Id, TokenKey: token.Key,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channel.Id}, OriginModelName: "display-model",
		UsingGroup: "default", UserGroup: "default", UserSetting: dto.UserSetting{BillingPreference: "wallet_only"},
		ForcePreConsume: true, StartTime: time.Now(), RelayFormat: types.RelayFormatOpenAI,
		PriceData:             hosttypes.PriceData{GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1}},
		TieredBillingSnapshot: snapshot,
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	ctx.Set("username", user.Username)
	require.Nil(t, PreConsumeBilling(ctx, reservation, info))

	// A later administrator edit must not change the in-flight log's unit price.
	cfg := config.GlobalConfig.Get("billing_setting")
	previous, err := config.ConfigToMap(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, config.UpdateConfigFromMap(cfg, previous)) })
	newDisplay, err := common.Marshal(map[string]string{"display-model": `tier("base", p * 10 + cr * 3)`})
	require.NoError(t, err)
	require.NoError(t, config.UpdateConfigFromMap(cfg, map[string]string{"display_billing_expr": string(newDisplay)}))
	PostTextConsumeQuota(ctx, info, &dto.Usage{
		PromptTokens: 1000, TotalTokens: 1000, PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 1000},
	}, nil)
	require.NoError(t, model.DB.First(&user, user.Id).Error)
	require.NoError(t, model.DB.First(&token, token.Id).Error)
	assert.Equal(t, int64(startingQuota-want), user.Quota)
	assert.Equal(t, startingQuota-want, token.RemainQuota)
	assert.Equal(t, int64(want), user.UsedQuota)

	var stored model.Log
	require.NoError(t, model.LOG_DB.Where("user_id = ?", user.Id).Take(&stored).Error)
	assert.Equal(t, want, stored.Quota)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(stored.Other, &other))
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte(display)), other["expr_b64"])
	adminInfo, ok := other["admin_info"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte(actual)), adminInfo["actual_expr_b64"])
	assert.Nil(t, other["cache_ratio"])
	userLogs, total, err := model.GetUserLogs(user.Id, model.LogTypeConsume, 0, 0, "", "", 0, 10, "", "", "")
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, userLogs, 1)
	assert.Equal(t, want, userLogs[0].Quota)
	assert.NotContains(t, userLogs[0].Other, "actual_expr_b64")
	assert.NotContains(t, userLogs[0].Other, "admin_info")
	stat, err := model.SumUsedQuota(model.LogTypeConsume, 0, 0, "", user.Username, "", 0, "")
	require.NoError(t, err)
	assert.Equal(t, want, stat.Quota)
}
