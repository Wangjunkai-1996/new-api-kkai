package model

import (
	"math"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUserBaseWriteContextPreservesInt64Quota(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	expected := int64(math.MaxInt32) + 5_000_000_000

	user := User{Quota: expected}
	user.ToBaseUser().WriteContext(context)

	require.Equal(t, expected, common.GetContextKeyInt64(context, constant.ContextKeyUserQuota))
}

func TestSubscriptionBalanceQuotaPreservesInt64Amount(t *testing.T) {
	quota, err := calcSubscriptionBalanceQuota(5_000)

	require.NoError(t, err)
	require.Equal(t, int64(2_500_000_000), quota)
}
