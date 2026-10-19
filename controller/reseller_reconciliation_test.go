package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestResellerConsumptionReconciliationRequiresPlatformEvidence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:reseller-reconciliation-controller?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TenantConsumption{}, &model.ResellerWallet{}, &model.ResellerLedgerEntry{}))
	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.Create(&model.User{Id: 1, TenantID: 42, Username: "customer", Quota: 900}).Error)
	require.NoError(t, db.Create(&model.ResellerWallet{TenantID: 42, ExactProfitCarry: "0", Version: 1}).Error)
	require.NoError(t, db.Create(&model.TenantConsumption{TenantID: 42, CustomerID: 1, RequestID: "uncertain", QuoteID: 1, Status: "reserved", ReservedQuota: 100, LastError: "upstream_final_usage_unknown"}).Error)
	require.NoError(t, db.Create(&model.TenantConsumption{TenantID: 42, CustomerID: 1, RequestID: "crash", QuoteID: 2, Status: "reserved", ReservedQuota: 100}).Error)
	require.NoError(t, db.Create(&model.TenantConsumption{TenantID: 42, CustomerID: 1, RequestID: "uncertain-confirmed", QuoteID: 3, Status: "reserved", ReservedQuota: 100, LastError: "upstream_final_usage_unknown"}).Error)
	for _, test := range []struct {
		name      string
		role      int
		tenant    int64
		body      string
		status    int
		requestID string
	}{
		{"owner", common.RoleCommonUser, 0, `{}`, http.StatusForbidden, "uncertain"},
		{"admin_on_tenant_host", common.RoleAdminUser, 42, `{}`, http.StatusForbidden, "uncertain"},
		{"missing_usage", common.RoleAdminUser, 0, `{"idempotency_key":"review","reason":"checked","evidence":"receipt"}`, http.StatusBadRequest, "uncertain"},
		{"unconfirmed_zero", common.RoleAdminUser, 0, `{"actual_quota":0,"idempotency_key":"review","reason":"checked","evidence":"receipt"}`, http.StatusBadRequest, "uncertain"},
		{"confirmed_usage", common.RoleAdminUser, 0, `{"actual_quota":100,"idempotency_key":"review","reason":"checked","evidence":"receipt","tenant_id":100,"actor_id":888}`, http.StatusOK, "uncertain"},
		{"same_payload_replay", common.RoleAdminUser, 0, `{"actual_quota":100,"idempotency_key":"review","reason":"checked","evidence":"receipt"}`, http.StatusOK, "uncertain"},
		{"conflicting_payload", common.RoleAdminUser, 0, `{"actual_quota":50,"idempotency_key":"review","reason":"checked","evidence":"receipt"}`, http.StatusConflict, "uncertain"},
		{"crash_without_execution_confirmation", common.RoleAdminUser, 0, `{"actual_quota":100,"idempotency_key":"crash-review","reason":"checked","evidence":"receipt"}`, http.StatusConflict, "crash"},
		{"crash_with_execution_confirmation", common.RoleAdminUser, 0, `{"actual_quota":100,"confirmed_execution_stopped":true,"idempotency_key":"crash-review","reason":"instance exited","evidence":"process exit and request drain proof"}`, http.StatusOK, "crash"},
		{"crash_changed_execution_confirmation", common.RoleAdminUser, 0, `{"actual_quota":100,"idempotency_key":"crash-review","reason":"instance exited","evidence":"process exit and request drain proof"}`, http.StatusConflict, "crash"},
		{"unknown_with_execution_confirmation", common.RoleAdminUser, 0, `{"confirmed_zero":true,"confirmed_execution_stopped":true,"idempotency_key":"unknown-reviewed","reason":"checked","evidence":"zero usage receipt"}`, http.StatusOK, "uncertain-confirmed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(test.body))
			ctx.Params = gin.Params{{Key: "id", Value: "42"}, {Key: "request_id", Value: test.requestID}}
			ctx.Set("id", 99)
			ctx.Set("role", test.role)
			common.SetContextKey(ctx, constant.ContextKeyTenantContext, model.TenantContext{TenantID: test.tenant})
			AdminReconcileResellerConsumption(ctx)
			assert.Equal(t, test.status, recorder.Code, recorder.Body.String())
		})
	}
	var audits []model.ResellerLedgerEntry
	require.NoError(t, db.Find(&audits).Error)
	require.Len(t, audits, 3)
	assert.EqualValues(t, 42, audits[0].TenantID)
	assert.Equal(t, 99, audits[0].ActorID)
	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	assert.EqualValues(t, 900, user.Quota)
}
