package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func setupDashboardAuthMiddlewareTest(t *testing.T) {
	t.Helper()
	dsn := os.Getenv("TEST_AUTH_CACHE_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("isolated loopback PostgreSQL fixture is not configured")
	}
	config, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	hosts := []string{config.Host}
	for _, fallback := range config.Fallbacks {
		hosts = append(hosts, fallback.Host)
	}
	for _, host := range hosts {
		require.Contains(t, []string{"127.0.0.1", "::1", "localhost"}, host, "auth fixture requires a loopback PostgreSQL host")
	}
	require.True(t, strings.HasPrefix(config.Database, "kkai_test_"), "auth fixture requires a dedicated kkai_test_ database")
	config.ConnectTimeout = 5 * time.Second
	config.RuntimeParams["statement_timeout"] = "5000"
	config.RuntimeParams["lock_timeout"] = "5000"
	admin := stdlib.OpenDB(*config)
	t.Cleanup(func() { require.NoError(t, admin.Close()) })
	schema := "auth_middleware_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = admin.ExecContext(ctx, "CREATE SCHEMA "+schema)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := admin.ExecContext(ctx, "DROP SCHEMA "+schema+" CASCADE")
		require.NoError(t, err)
	})
	dataConfig := config.Copy()
	dataConfig.RuntimeParams["search_path"] = schema
	pool := stdlib.OpenDB(*dataConfig)
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}, &model.AuditLog{}, &model.UserAccessToken{}, &model.Option{}))

	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis, previousSecret := common.RedisEnabled, common.SessionSecret
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousType, previousLogType)
		common.RedisEnabled, common.SessionSecret = previousRedis, previousSecret
	})
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypePostgreSQL, common.DatabaseTypePostgreSQL)
	common.RedisEnabled = false
	common.SessionSecret = "middleware-auth-test-secret"
	require.NoError(t, model.EnsureLegacyAccessTokenRetireAt(time.Now().Unix()))
}

func issueExpiredDashboardAccessToken(t *testing.T, identity service.AuthIdentity) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss": "new-api", "aud": []string{"new-api-dashboard"},
		"sub": fmt.Sprintf("%d", identity.UserID), "token_use": "access",
		"sid": identity.SessionID, "uv": identity.UserAuthVersion, "sv": identity.SessionVersion,
		"exp": time.Now().Add(-time.Minute).Unix(),
		"nbf": time.Now().Add(-2 * time.Minute).Unix(),
		"iat": time.Now().Add(-2 * time.Minute).Unix(),
	}
	mac := hmac.New(sha256.New, []byte(common.SessionSecret))
	_, err := mac.Write([]byte("new-api/auth/access/v1"))
	require.NoError(t, err)
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(mac.Sum(nil))
	require.NoError(t, err)
	return token
}

func tamperDashboardToken(token string) string {
	tamperAt := len(token) - 2
	replacement := "x"
	if token[tamperAt] == 'x' {
		replacement = "y"
	}
	return token[:tamperAt] + replacement + token[tamperAt+1:]
}

func middlewareBearerRequest(router *gin.Engine, path, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
