package controller

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func openRC49ControllerPostgres(t *testing.T) *gorm.DB {
	t.Helper()
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
	schema := "rc49_token_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NoError(t, admin.Exec("CREATE SCHEMA "+schema).Error)
	t.Cleanup(func() { require.NoError(t, admin.Exec("DROP SCHEMA "+schema+" CASCADE").Error) })
	db, err := gorm.Open(postgres.Open(raw+"&search_path="+schema), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	return db
}

func TestTokenQuotaPermissionDefaultsAndPrivacyPostgres(t *testing.T) {
	db := openRC49ControllerPostgres(t)
	oldDB, oldLog := model.DB, model.LOG_DB
	oldRedis := common.RedisEnabled
	oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	model.DB, model.LOG_DB, common.RedisEnabled = db, db, false
	common.SetDatabaseTypes(common.DatabaseTypePostgreSQL, common.DatabaseTypePostgreSQL)
	previousMode := common.SchemaManagementMode
	common.SchemaManagementMode = common.SchemaManagementExternal
	t.Setenv("LOG_SQL_DSN", "")
	require.NoError(t, model.InitLogDB())
	common.SchemaManagementMode = previousMode

	t.Cleanup(func() {
		model.DB, model.LOG_DB, common.RedisEnabled = oldDB, oldLog, oldRedis
		common.SetDatabaseTypes(oldMainType, oldLogType)
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}))
	user := model.User{Username: "token-owner", Status: common.UserStatusEnabled, Group: "default", Quota: 1 << 40, UsedQuota: 123}
	require.NoError(t, db.Create(&user).Error)
	for _, tc := range []struct {
		name       string
		permission *bool
		want       bool
	}{{"default", nil, true}, {"denied", common.GetPointer(false), false}, {"allowed", common.GetPointer(true), true}} {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{"name": tc.name, "expired_time": -1, "unlimited_quota": true}
			if tc.permission != nil {
				body["allow_user_quota_query"] = *tc.permission
			}
			ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "/api/token/", body, user.Id)
			AddToken(ctx)
			require.True(t, decodeAPIResponse(t, recorder).Success, recorder.Body.String())
			var token model.Token
			require.NoError(t, db.Where("name = ?", tc.name).First(&token).Error)
			assert.Equal(t, tc.want, token.AllowUserQuotaQuery)
			body["id"] = token.Id
			delete(body, "allow_user_quota_query")
			ctx, recorder = newAuthenticatedContext(t, http.MethodPut, "/api/token/", body, user.Id)
			UpdateToken(ctx)
			require.True(t, decodeAPIResponse(t, recorder).Success, recorder.Body.String())
			require.NoError(t, db.First(&token, token.Id).Error)
			assert.Equal(t, tc.want, token.AllowUserQuotaQuery, "omitted update retains the permission")
			ctx, recorder = newAuthenticatedContext(t, http.MethodGet, "/api/usage/token", nil, user.Id)
			ctx.Request.Header.Set("Authorization", "Bearer "+token.Key)
			GetTokenUsage(ctx)
			require.Equal(t, http.StatusOK, recorder.Code)
			var response struct {
				Data map[string]any `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			for _, key := range []string{"user_quota", "user_total_available", "user_total_used", "user_total_granted"} {
				_, exists := response.Data[key]
				assert.Equal(t, tc.want, exists, key)
			}
			if tc.want {
				assert.Equal(t, float64(1<<40), response.Data["user_total_available"])
			}
		})
	}
}
