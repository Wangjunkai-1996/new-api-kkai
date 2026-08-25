package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetChannelClearsAdminRejectReasonBeforeRetryHandler(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyChannelId, 11)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(c, constant.ContextKeyChannelName, "first")
	common.SetContextKey(c, constant.ContextKeyChannelAutoBan, false)
	retry := &service.RetryParam{
		Ctx:   c,
		Retry: common.GetPointer(0),
	}
	info := &relaycommon.RelayInfo{}

	firstChannel, firstErr := getChannel(c, info, retry)
	require.Nil(t, firstErr)
	require.Equal(t, 11, firstChannel.Id)
	common.SetContextKey(c, constant.ContextKeyAdminRejectReason, "gemini_empty_candidates")

	retry.IncreaseRetry()
	secondChannel, secondErr := getChannel(c, info, retry)

	require.Nil(t, secondErr)
	require.Equal(t, 11, secondChannel.Id)
	require.Equal(t, 1, retry.GetRetry())
	require.Empty(t, common.GetContextKeyString(c, constant.ContextKeyAdminRejectReason))
}
