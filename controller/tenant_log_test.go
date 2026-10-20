package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantSelfLogStatsUseIdentityInsteadOfSharedUsername(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	for _, row := range []model.Log{
		{TenantID: 42, UserId: 1, Username: "same-user", Type: model.LogTypeConsume, Quota: 10, PromptTokens: 7, CompletionTokens: 3, CreatedAt: time.Now().Unix()},
		{TenantID: 43, UserId: 2, Username: "same-user", Type: model.LogTypeConsume, Quota: 900, PromptTokens: 800, CompletionTokens: 100, CreatedAt: time.Now().Unix()},
	} {
		require.NoError(t, db.Create(&row).Error)
	}
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/log/self/stat", nil)
	c.Set("id", 1)
	c.Set("username", "same-user")
	GetLogsSelfStat(c)
	var payload struct {
		Success bool       `json:"success"`
		Data    model.Stat `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
	require.True(t, payload.Success, response.Body.String())
	assert.Equal(t, 10, payload.Data.Quota)
	assert.Equal(t, 1, payload.Data.Rpm)
	assert.Equal(t, 10, payload.Data.Tpm)
}

func TestTenantSelfAuditLogsUseIdentityAndHidePrivilegedMetadata(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.AuditLog{}))
	for _, row := range []model.AuditLog{
		{EventId: "own", UserId: 1, Username: "same-user", ActorRole: common.RoleCommonUser, Category: model.AuditCategorySecurity, Other: model.AuditOther{AdminInfo: &model.AuditAdminInfo{AdminID: 99}}},
		{EventId: "other-tenant", UserId: 2, Username: "same-user", ActorRole: common.RoleCommonUser, Category: model.AuditCategorySecurity},
	} {
		require.NoError(t, db.Create(&row).Error)
	}
	engine := gin.New()
	engine.GET("/api/audit/self", func(c *gin.Context) {
		c.Set("id", 1)
		c.Set("role", common.RoleCommonUser)
		GetAuditLogs(c)
	})
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/audit/self?username=other-tenant", nil))
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Items []model.AuditLog `json:"items"`
			Total int              `json:"total"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
	require.True(t, payload.Success, response.Body.String())
	require.Len(t, payload.Data.Items, 1)
	assert.Equal(t, 1, payload.Data.Total)
	assert.Equal(t, "own", payload.Data.Items[0].EventId)
	assert.Nil(t, payload.Data.Items[0].Other.AdminInfo)
}
