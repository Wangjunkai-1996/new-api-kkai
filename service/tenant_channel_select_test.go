package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantChannelSelectionRejectsUnverifiedTransports(t *testing.T) {
	db := newImageStudioChannelSelectionTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Tenant{}, &model.TenantCatalogEntry{}))
	const group, modelName = "tenant-supply", "shared-model"
	require.NoError(t, db.Create(&model.Tenant{ID: 7, Status: model.TenantStatusActive}).Error)
	require.NoError(t, db.Create(&model.TenantCatalogEntry{TenantID: 7, Model: modelName, SalesGroup: group, Status: model.TenantCatalogStatusEnabled, IsDefault: true}).Error)
	sdk := seedImageStudioSelectionChannel(t, db, group, modelName, constant.ChannelTypeAws, 100)
	seedImageStudioSelectionChannel(t, db, group, modelName, constant.ChannelTypeXunfei, 90)
	safe := seedImageStudioSelectionChannel(t, db, group, modelName, constant.ChannelTypeOpenAI, 10)
	for _, cached := range []bool{false, true} {
		t.Run(fmt.Sprintf("cache_%t", cached), func(t *testing.T) {
			common.MemoryCacheEnabled = cached
			if cached {
				require.NoError(t, model.SyncChannelCacheOnce())
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			constraints := GetChannelConstraints(c)
			common.SetContextKey(c, constant.ContextKeyTenantID, int64(7))
			retry := &RetryParam{Ctx: c, TokenGroup: group, ModelName: modelName, RequestPath: c.Request.URL.Path}
			selected, _, err := SelectChannelForRequest(c, modelName, retry)
			require.Nil(t, err)
			require.NotNil(t, selected)
			assert.Equal(t, safe.Id, selected.Id, "unsafe higher-priority siblings cannot receive tenant traffic")
			selected, _, randomErr := CacheGetRandomSatisfiedChannel(retry)
			require.NoError(t, randomErr)
			require.NotNil(t, selected)
			assert.Equal(t, safe.Id, selected.Id, "direct retry selection retains the transport filter")
			constraints.AddPin(dto.ChannelPin{ChannelId: sdk.Id, Source: dto.PinSourceToken, Rank: dto.PinRankToken})
			selected, _, err = SelectChannelForRequest(c, modelName, retry)
			assert.Nil(t, selected)
			require.NotNil(t, err)
			assert.Equal(t, dto.FilterAllowedChannelTypes, err.FilterKind)
		})
	}
}
