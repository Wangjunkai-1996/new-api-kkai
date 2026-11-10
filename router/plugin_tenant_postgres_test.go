package router

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestNativePluginDispatchPreservesTrustedTenantBeforeTokenAuth(t *testing.T) {
	raw := os.Getenv("TEST_AUTH_CACHE_POSTGRES_DSN")
	if raw == "" {
		t.Skip("isolated local PostgreSQL fixture is not enabled")
	}
	config, err := pgx.ParseConfig(raw)
	require.NoError(t, err)
	require.Contains(t, []string{"127.0.0.1", "localhost", "::1"}, config.Host)
	require.True(t, strings.HasPrefix(config.Database, "kkai_test_"))
	admin, err := gorm.Open(postgres.Open(raw), &gorm.Config{})
	require.NoError(t, err)
	adminSQL, err := admin.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, adminSQL.Close()) })
	schema := "rc49_router_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NoError(t, admin.Exec("CREATE SCHEMA "+schema).Error)
	t.Cleanup(func() { require.NoError(t, admin.Exec("DROP SCHEMA "+schema+" CASCADE").Error) })
	db, err := gorm.Open(postgres.Open(raw+"&search_path="+schema), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	oldDB, oldRedis := model.DB, common.RedisEnabled
	oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	model.DB, common.RedisEnabled = db, false
	common.SetDatabaseTypes(common.DatabaseTypePostgreSQL, common.DatabaseTypePostgreSQL)
	previousMode := common.SchemaManagementMode
	common.SchemaManagementMode = common.SchemaManagementExternal
	t.Setenv("LOG_SQL_DSN", "")
	require.NoError(t, model.InitLogDB())
	common.SchemaManagementMode = previousMode

	t.Cleanup(func() {
		model.DB, common.RedisEnabled = oldDB, oldRedis
		common.SetDatabaseTypes(oldMainType, oldLogType)
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}))
	require.NoError(t, i18n.Init())
	for _, tenant := range []int64{0, 7} {
		user := model.User{TenantID: tenant, Username: []string{"platform-user", "tenant-user"}[tenant/7], Status: common.UserStatusEnabled, Group: "default", Quota: 100}
		user.AffCode = user.Username
		require.NoError(t, db.Create(&user).Error)
		require.NoError(t, db.Create(&model.Token{TenantID: tenant, UserId: user.Id, Key: []string{"platformsecret", "tenantsecret"}[tenant/7], Status: common.TokenStatusEnabled, ExpiredTime: -1, UnlimitedQuota: true}).Error)
	}
	plugin := compileRouterPlugin(t, "tenant-native", "1.0.0", `[{method:"GET",path:"/vendor/native",type:"dynamic"}]`)
	handlers := func(generation *jsplugin.RoutingGeneration, group jsplugin.RouteGroup) []gin.HandlerFunc {
		production := productionPluginRouteHandlers(generation, group)
		return []gin.HandlerFunc{production[0], production[1], production[2], func(c *gin.Context) {
			assert.Equal(t, int64(0), common.GetContextKeyInt64(c, constant.ContextKeyTenantID))
			assert.Equal(t, "platform.test", common.GetContextKeyString(c, constant.ContextKeyTenantHost))
			c.String(http.StatusOK, "authorized")
		}}
	}
	outer, registry := newPluginRouterTest(t, []*jsplugin.LoadedPlugin{plugin}, handlers)
	outer.Use(middleware.TenantRouting(nil, false, "platform.test"))
	outer.NoRoute((&pluginRouteDispatcher{registry: registry}).dispatch)
	for _, tc := range []struct {
		key    string
		status int
	}{{"platformsecret", http.StatusOK}, {"tenantsecret", http.StatusForbidden}} {
		request := httptest.NewRequest(http.MethodGet, "http://platform.test/vendor/native?tenant_id=7", nil)
		request.Header.Set("Authorization", "Bearer "+tc.key)
		request.Header.Set("X-Tenant-ID", "7")
		request.Header.Set("X-Forwarded-Host", "tenant.test")
		recorder := httptest.NewRecorder()
		outer.ServeHTTP(recorder, request)
		assert.Equal(t, tc.status, recorder.Code, recorder.Body.String())
		assert.NotContains(t, recorder.Body.String(), tc.key)
	}
}
