package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupVideoProxyTestDB(t *testing.T) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	originalMemoryCache := common.MemoryCacheEnabled
	originalRedisEnabled := common.RedisEnabled
	originalFetchSetting := *system_setting.GetFetchSetting()
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalDB := model.DB
	originalLOGDB := model.LOG_DB
	originalSQLDSN, hadSQLDSN := os.LookupEnv("SQL_DSN")

	common.MemoryCacheEnabled = false
	common.RedisEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	*system_setting.GetFetchSetting() = system_setting.FetchSetting{
		EnableSSRFProtection:   true,
		AllowPrivateIp:         false,
		DomainFilterMode:       false,
		IpFilterMode:           false,
		DomainList:             []string{},
		IpList:                 []string{},
		AllowedPorts:           []string{"80", "443"},
		ApplyIPFilterForDomain: true,
	}

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Channel{}))
	service.InitHttpClient()

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
		common.MemoryCacheEnabled = originalMemoryCache
		common.RedisEnabled = originalRedisEnabled
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		model.DB = originalDB
		model.LOG_DB = originalLOGDB
		*system_setting.GetFetchSetting() = originalFetchSetting
		if hadSQLDSN {
			require.NoError(t, os.Setenv("SQL_DSN", originalSQLDSN))
		} else {
			require.NoError(t, os.Unsetenv("SQL_DSN"))
		}
		service.InitHttpClient()
	})
}

func TestVideoProxyAllowsProviderManagedPrivateBaseURL(t *testing.T) {
	tests := []struct {
		name        string
		channelType int
	}{
		{name: "sora", channelType: constant.ChannelTypeSora},
		{name: "openai", channelType: constant.ChannelTypeOpenAI},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupVideoProxyTestDB(t)

			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/v1/videos/videos_test/content", r.URL.Path)
				require.Equal(t, "Bearer channel-key", r.Header.Get("Authorization"))
				require.Equal(t, "bytes=0-3", r.Header.Get("Range"))
				w.Header().Set("Content-Type", "video/mp4")
				w.Header().Set("Content-Range", "bytes 0-3/9")
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write([]byte("mock"))
			}))
			t.Cleanup(upstream.Close)

			channel := model.Channel{Id: 60, Type: tt.channelType, Key: "channel-key", BaseURL: common.GetPointer(upstream.URL)}
			require.NoError(t, model.DB.Create(&channel).Error)
			require.NoError(t, model.DB.Create(&model.Task{
				TaskID:    "task_public",
				UserId:    1,
				ChannelId: 60,
				Status:    model.TaskStatusSuccess,
				PrivateData: model.TaskPrivateData{
					UpstreamTaskID: "videos_test",
				},
			}).Error)

			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set("id", 1)
				c.Next()
			})
			router.GET("/v1/videos/:task_id/content", VideoProxy)

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/v1/videos/task_public/content", nil)
			request.Header.Set("Range", "bytes=0-3")
			router.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusPartialContent, recorder.Code)
			assert.Equal(t, "video/mp4", recorder.Header().Get("Content-Type"))
			assert.Equal(t, "bytes 0-3/9", recorder.Header().Get("Content-Range"))
			assert.Equal(t, "mock", recorder.Body.String())
		})
	}
}

func TestVideoProxyStillBlocksStoredPrivateResultURL(t *testing.T) {
	setupVideoProxyTestDB(t)

	privateURL := "http://127.0.0.1/private-video.mp4"
	require.NoError(t, model.DB.Create(&model.Channel{Id: 61, Type: constant.ChannelTypeCustom, Key: "channel-key"}).Error)
	require.NoError(t, model.DB.Create(&model.Task{
		TaskID:      "task_private_result",
		UserId:      1,
		ChannelId:   61,
		Status:      model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{ResultURL: privateURL},
	}).Error)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("id", 1)
		c.Next()
	})
	router.GET("/v1/videos/:task_id/content", VideoProxy)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/videos/task_private_result/content", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "request blocked")
	assert.NotContains(t, recorder.Body.String(), privateURL)
}

func TestVideoProxyRejectsInvalidRangeBeforeUpstreamFetch(t *testing.T) {
	setupVideoProxyTestDB(t)

	upstreamURL, err := url.Parse("http://127.0.0.1:8080")
	require.NoError(t, err)
	channel := model.Channel{Id: 62, Type: constant.ChannelTypeSora, Key: "channel-key", BaseURL: common.GetPointer(upstreamURL.String())}
	require.NoError(t, model.DB.Create(&channel).Error)
	require.NoError(t, model.DB.Create(&model.Task{
		TaskID:      "task_bad_range",
		UserId:      1,
		ChannelId:   62,
		Status:      model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{UpstreamTaskID: "videos_test"},
	}).Error)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("id", 1)
		c.Next()
	})
	router.GET("/v1/videos/:task_id/content", VideoProxy)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/videos/task_bad_range/content", nil)
	request.Header.Set("Range", "bytes=0-3,8-9")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "range header is invalid")
}

func TestVideoProxyRangeHeader(t *testing.T) {
	tests := []struct {
		name        string
		values      []string
		expected    string
		expectError bool
	}{
		{name: "absent"},
		{name: "closed range", values: []string{"bytes=0-1023"}, expected: "bytes=0-1023"},
		{name: "open ended range", values: []string{"bytes=1024-"}, expected: "bytes=1024-"},
		{name: "suffix range", values: []string{"bytes=-1024"}, expected: "bytes=-1024"},
		{name: "trims whitespace", values: []string{" bytes=0-1 "}, expected: "bytes=0-1"},
		{name: "rejects multiple header values", values: []string{"bytes=0-1", "bytes=2-3"}, expectError: true},
		{name: "rejects multi range", values: []string{"bytes=0-1,2-3"}, expectError: true},
		{name: "rejects unknown unit", values: []string{"items=0-1"}, expectError: true},
		{name: "rejects oversized value", values: []string{"bytes=" + strings.Repeat("1", 129) + "-"}, expectError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header := http.Header{}
			for _, value := range tt.values {
				header.Add("Range", value)
			}

			actual, err := videoProxyRangeHeader(header)
			if tt.expectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, actual)
		})
	}
}
