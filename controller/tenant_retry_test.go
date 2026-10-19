package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantRetryRevalidatesCatalogWithoutChangingQuotedGroup(t *testing.T) {
	for _, change := range []string{"disable_tenant", "revoke_model", "change_group"} {
		t.Run(change, func(t *testing.T) {
			db := setupModelListControllerTestDB(t)
			previousCache := common.MemoryCacheEnabled
			common.MemoryCacheEnabled = false
			t.Cleanup(func() { common.MemoryCacheEnabled = previousCache })
			require.NoError(t, db.AutoMigrate(&model.Tenant{}, &model.TenantCatalogEntry{}))
			require.NoError(t, db.Create(&model.Tenant{ID: 42, Status: model.TenantStatusActive}).Error)
			require.NoError(t, db.Create(&model.TenantCatalogEntry{TenantID: 42, SalesGroup: "default", Model: "text", Status: model.TenantCatalogStatusEnabled, IsDefault: true}).Error)
			require.NoError(t, db.Create(&model.Channel{Id: 1, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Key: "test-key", Group: "default", Models: "text"}).Error)
			require.NoError(t, db.Create(&model.Ability{ChannelId: 1, Group: "default", Model: "text", Enabled: true}).Error)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			common.SetContextKey(c, constant.ContextKeyTenantID, int64(42))
			info := &relaycommon.RelayInfo{TenantID: 42, TenantQuoteID: 17, TenantPriceVersionID: 1, TenantRetailMultiplier: 2, UsingGroup: "default", OriginModelName: "text", ChannelMeta: &relaycommon.ChannelMeta{}}
			retry := &service.RetryParam{Ctx: c, TokenGroup: "default", ModelName: "text"}
			channel, apiErr := getChannel(c, info, retry)
			require.Nil(t, apiErr)
			require.NotNil(t, channel)
			assert.Equal(t, 1, channel.Id)
			switch change {
			case "disable_tenant":
				require.NoError(t, db.Model(&model.Tenant{}).Where("id = ?", 42).Update("status", model.TenantStatusDisabled).Error)
			case "revoke_model":
				require.NoError(t, db.Model(&model.TenantCatalogEntry{}).Where("tenant_id = ?", 42).Update("status", model.TenantCatalogStatusDisabled).Error)
			case "change_group":
				require.NoError(t, db.Model(&model.TenantCatalogEntry{}).Where("tenant_id = ?", 42).Update("sales_group", "new-supply").Error)
			}
			retry.IncreaseRetry()
			channel, apiErr = getChannel(c, info, retry)
			assert.Nil(t, channel)
			require.NotNil(t, apiErr)
			assert.True(t, types.IsSkipRetryError(apiErr))
			assert.Equal(t, "default", retry.TokenGroup)
			assert.Equal(t, "default", info.UsingGroup)
			assert.EqualValues(t, 17, info.TenantQuoteID)
			assert.Equal(t, 2.0, info.PriceData.GroupRatioInfo.GroupRatio)
		})
	}
}
