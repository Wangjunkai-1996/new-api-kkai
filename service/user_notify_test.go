package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHighPrioritySecurityNotificationBypassesOrdinaryLimit(t *testing.T) {
	previousRedisEnabled := common.RedisEnabled
	previousLimit := constant.NotifyLimitCount
	common.RedisEnabled = false
	constant.NotifyLimitCount = 0
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
		constant.NotifyLimitCount = previousLimit
	})

	setting := dto.UserSetting{NotifyType: dto.NotifyTypeEmail}
	notification := dto.NewNotify("policy_incident", "security event", "review required", nil)

	err := NotifyUser(987654321, "", setting, notification)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "notification limit exceeded")

	assert.NoError(t, notifyUser(987654321, "", setting, notification, false))
}
