package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/wsmanager"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelDisableClosesOnlyAffectedWebSockets(t *testing.T) {
	setupTaskPluginBindChannelTest(t)
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI, Key: "test-key", Status: common.ChannelStatusEnabled, Name: "websocket channel", Group: "default", Models: "gpt-5"}
	require.NoError(t, channel.Insert())
	closed := map[string]string{}
	t.Cleanup(wsmanager.Register(channel.Id, wsmanager.KindResponses, func(reason string) { closed["responses"] = reason }))
	t.Cleanup(wsmanager.Register(channel.Id, wsmanager.KindRealtime, func(reason string) { closed["realtime"] = reason }))
	t.Cleanup(wsmanager.Register(channel.Id+1000, wsmanager.KindResponses, func(reason string) { closed["other"] = reason }))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Params = gin.Params{{Key: "id", Value: strconv.Itoa(channel.Id)}}
	c.Set("role", common.RoleRootUser)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/channel/status", strings.NewReader(`{"status":2}`))
	c.Request.Header.Set("Content-Type", "application/json")
	UpdateChannelStatus(c)
	updated, err := model.GetChannelById(channel.Id, false)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, updated.Status)
	assert.Equal(t, map[string]string{"responses": service.ChannelDisabledCloseReason, "realtime": service.ChannelDisabledCloseReason}, closed)
}

func TestManageMultiKeysClosesOnExhaustionAndRestoresAvailability(t *testing.T) {
	setupTaskPluginBindChannelTest(t)
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI, Key: "first-key\nsecond-key", Status: common.ChannelStatusEnabled,
		Name: "multi-key websocket channel", Group: "default", Models: "gpt-5",
		ChannelInfo: model.ChannelInfo{IsMultiKey: true, MultiKeySize: 2}}
	require.NoError(t, channel.Insert())
	closed := 0
	t.Cleanup(wsmanager.Register(channel.Id, wsmanager.KindResponses, func(string) { closed++ }))
	for _, step := range []struct {
		action string
		key    int
		status int
		closes int
	}{
		{"disable_key", 0, common.ChannelStatusEnabled, 0},
		{"disable_key", 1, common.ChannelStatusManuallyDisabled, 1},
		{"enable_key", 0, common.ChannelStatusEnabled, 1},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set("role", common.RoleRootUser)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/channel/multi_key", strings.NewReader(fmt.Sprintf(`{"channel_id":%d,"action":%q,"key_index":%d}`, channel.Id, step.action, step.key)))
		c.Request.Header.Set("Content-Type", "application/json")
		ManageMultiKeys(c)
		updated, err := model.GetChannelById(channel.Id, true)
		require.NoError(t, err)
		assert.Equal(t, step.status, updated.Status, step.action)
		assert.Equal(t, step.closes, closed, step.action)
	}
}
