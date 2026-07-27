package controller

import (
	"bytes"
	"fmt"
	"io"
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

type videoProxyTestRoundTripper func(*http.Request) (*http.Response, error)

func (transport videoProxyTestRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

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

func TestVideoProxyRejectsAssetHostedTaskBeforeUpstreamLookup(t *testing.T) {
	setupVideoProxyTestDB(t)

	upstreamCalls := 0
	client := service.GetHttpClient()
	originalTransport := client.Transport
	client.Transport = videoProxyTestRoundTripper(func(request *http.Request) (*http.Response, error) {
		upstreamCalls++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"video/mp4"}},
			Body:       io.NopCloser(strings.NewReader("private video")),
			Request:    request,
		}, nil
	})
	t.Cleanup(func() { client.Transport = originalTransport })

	upstreamURL := "https://provider.example"
	channel := model.Channel{Id: 63, Type: constant.ChannelTypeSora, Key: "channel-key", BaseURL: common.GetPointer(upstreamURL)}
	require.NoError(t, model.DB.Create(&channel).Error)
	require.NoError(t, model.DB.Create(&model.Task{
		TaskID: "task_asset_hosted", UserId: 1, ChannelId: channel.Id, Status: model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID:    "videos_private",
			ResultURL:         upstreamURL + "/private.mp4",
			ArchiveSource:     upstreamURL + "/private.mp4",
			AssetHostedResult: true,
		},
	}).Error)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("id", 1)
		c.Next()
	})
	router.GET("/v1/videos/:task_id/content", VideoProxy)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/videos/task_asset_hosted/content", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusNotFound, recorder.Code)
	assert.Zero(t, upstreamCalls)
	assert.NotContains(t, recorder.Body.String(), upstreamURL)
}

func TestProviderVideoURLHelpersRejectAssetHostedTasks(t *testing.T) {
	task := &model.Task{
		TaskID: "task_asset_hosted_helper",
		Data:   []byte(`{"uri":"https://provider.example/private.mp4"}`),
		PrivateData: model.TaskPrivateData{
			ResultURL:         "https://provider.example/private.mp4",
			AssetHostedResult: true,
		},
	}

	tests := []struct {
		name    string
		resolve func() (string, error)
	}{
		{
			name: "gemini",
			resolve: func() (string, error) {
				resolved, _, err := getGeminiVideoURL(&model.Channel{Type: constant.ChannelTypeGemini}, task, "private-key")
				return resolved, err
			},
		},
		{
			name: "vertex",
			resolve: func() (string, error) {
				return getVertexVideoURL(&model.Channel{Type: constant.ChannelTypeVertexAi}, task)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved, err := tt.resolve()
			require.Error(t, err)
			assert.Empty(t, resolved)
			assert.NotContains(t, err.Error(), "provider.example")
			assert.NotContains(t, err.Error(), "private-key")
		})
	}
}

func TestGetGeminiVideoURLScopesCredentialsToProviderOrigin(t *testing.T) {
	channel := &model.Channel{
		Type:    constant.ChannelTypeGemini,
		BaseURL: common.GetPointer("https://generativelanguage.googleapis.com"),
	}
	tests := []struct {
		name       string
		videoURL   string
		wantAPIKey bool
	}{
		{name: "same origin", videoURL: "https://generativelanguage.googleapis.com/v1beta/files/video?key=stale-key&api_key=stale-api-key&alt=media", wantAPIKey: true},
		{name: "cross origin", videoURL: "https://media.example/private.mp4"},
		{name: "cross origin opaque credential-like query", videoURL: "https://media.example/private.mp4?key=cdn-object-key&api_key=cdn-signature&alt=media"},
		{name: "plaintext same host", videoURL: "http://generativelanguage.googleapis.com/private.mp4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &model.Task{Data: []byte(fmt.Sprintf(`{"uri":%q}`, tt.videoURL))}
			resolved, useAPIKey, err := getGeminiVideoURL(channel, task, "private-key")
			require.NoError(t, err)
			assert.Equal(t, tt.wantAPIKey, useAPIKey)
			if tt.wantAPIKey {
				assert.NotContains(t, resolved, "private-key")
				assert.NotContains(t, resolved, "stale-key")
				assert.NotContains(t, resolved, "stale-api-key")
				assert.Contains(t, resolved, "alt=media")
			} else {
				assert.Equal(t, tt.videoURL, resolved)
				assert.NotContains(t, resolved, "private-key")
			}
		})
	}
}

func TestVideoProxyPreservesCrossOriginGeminiMediaQueryWithoutProviderHeader(t *testing.T) {
	setupVideoProxyTestDB(t)
	system_setting.GetFetchSetting().EnableSSRFProtection = false

	providerAPIKey := "provider-secret"
	mediaURL := "https://media.example/private.mp4?key=cdn-object-key&api_key=cdn-signature&alt=media"
	channel := model.Channel{
		Id:      69,
		Type:    constant.ChannelTypeGemini,
		Key:     "channel-key",
		BaseURL: common.GetPointer("https://generativelanguage.googleapis.com"),
	}
	require.NoError(t, model.DB.Create(&channel).Error)
	require.NoError(t, model.DB.Create(&model.Task{
		TaskID:    "task_gemini_cross_origin_query",
		UserId:    1,
		ChannelId: channel.Id,
		Status:    model.TaskStatusSuccess,
		Data:      []byte(fmt.Sprintf(`{"uri":%q}`, mediaURL)),
		PrivateData: model.TaskPrivateData{
			Key: providerAPIKey,
		},
	}).Error)

	client := service.GetHttpClient()
	originalTransport := client.Transport
	requestedURL := ""
	providerHeader := ""
	client.Transport = videoProxyTestRoundTripper(func(request *http.Request) (*http.Response, error) {
		requestedURL = request.URL.String()
		providerHeader = request.Header.Get("x-goog-api-key")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"video/mp4"}},
			Body:       io.NopCloser(strings.NewReader("video")),
			Request:    request,
		}, nil
	})
	t.Cleanup(func() { client.Transport = originalTransport })

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("id", 1)
		c.Next()
	})
	router.GET("/v1/videos/:task_id/content", VideoProxy)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/videos/task_gemini_cross_origin_query/content", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "video", recorder.Body.String())
	assert.Equal(t, mediaURL, requestedURL)
	assert.Empty(t, providerHeader)
	assert.NotContains(t, requestedURL, providerAPIKey)
}

func TestVideoProxyScopesGeminiAPIKeyAcrossRedirects(t *testing.T) {
	tests := []struct {
		name            string
		redirectURL     string
		wantRedirectKey string
	}{
		{
			name:            "same origin keeps provider header",
			redirectURL:     "https://generativelanguage.googleapis.com/v1beta/files/video:download?alt=media",
			wantRedirectKey: "provider-secret",
		},
		{
			name:        "cross origin strips provider header",
			redirectURL: "https://media.example/private.mp4?alt=media",
		},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setupVideoProxyTestDB(t)
			system_setting.GetFetchSetting().EnableSSRFProtection = false

			const providerAPIKey = "provider-secret"
			const baseURL = "https://generativelanguage.googleapis.com"
			initialURL := baseURL + "/v1beta/files/video?alt=media"
			channelID := 690 + index
			taskID := fmt.Sprintf("task_gemini_redirect_%d", index)
			require.NoError(t, model.DB.Create(&model.Channel{
				Id: channelID, Type: constant.ChannelTypeGemini, Key: "channel-key", BaseURL: common.GetPointer(baseURL),
			}).Error)
			require.NoError(t, model.DB.Create(&model.Task{
				TaskID: taskID, UserId: 1, ChannelId: channelID, Status: model.TaskStatusSuccess,
				Data:        []byte(fmt.Sprintf(`{"uri":%q}`, initialURL)),
				PrivateData: model.TaskPrivateData{Key: providerAPIKey},
			}).Error)

			client := service.GetHttpClient()
			originalTransport := client.Transport
			requestCount := 0
			redirectKey := ""
			client.Transport = videoProxyTestRoundTripper(func(request *http.Request) (*http.Response, error) {
				requestCount++
				switch requestCount {
				case 1:
					assert.Equal(t, initialURL, request.URL.String())
					assert.Equal(t, providerAPIKey, request.Header.Get("x-goog-api-key"))
					return &http.Response{
						StatusCode: http.StatusFound,
						Header:     http.Header{"Location": []string{test.redirectURL}},
						Body:       io.NopCloser(strings.NewReader("")),
						Request:    request,
					}, nil
				case 2:
					redirectKey = request.Header.Get("x-goog-api-key")
					assert.Equal(t, test.redirectURL, request.URL.String())
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"video/mp4"}},
						Body:       io.NopCloser(strings.NewReader("video")),
						Request:    request,
					}, nil
				default:
					return nil, fmt.Errorf("unexpected request %d", requestCount)
				}
			})
			t.Cleanup(func() { client.Transport = originalTransport })

			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set("id", 1)
				c.Next()
			})
			router.GET("/v1/videos/:task_id/content", VideoProxy)

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/v1/videos/"+taskID+"/content", nil)
			router.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusOK, recorder.Code)
			assert.Equal(t, "video", recorder.Body.String())
			assert.Equal(t, 2, requestCount)
			assert.Equal(t, test.wantRedirectKey, redirectKey)
		})
	}
}

func TestVideoProxyNeverLeaksGeminiAPIKeyThroughURLLogsOrResponse(t *testing.T) {
	tests := []struct {
		name      string
		transport videoProxyTestRoundTripper
	}{
		{
			name: "request error",
			transport: func(request *http.Request) (*http.Response, error) {
				return nil, fmt.Errorf("dial failed for %s", request.URL.String())
			},
		},
		{
			name: "non-2xx response",
			transport: func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusBadGateway,
					Header:     http.Header{"Content-Type": []string{"text/plain"}},
					Body:       io.NopCloser(strings.NewReader("upstream failed")),
					Request:    request,
				}, nil
			},
		},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setupVideoProxyTestDB(t)
			system_setting.GetFetchSetting().ApplyIPFilterForDomain = false

			apiKey := fmt.Sprintf("gemini-secret-%d", index)
			baseURL := "https://generativelanguage.googleapis.com"
			videoURL := baseURL + "/v1beta/files/video?key=" + apiKey + "&alt=media"
			channelID := 70 + index
			require.NoError(t, model.DB.Create(&model.Channel{
				Id: channelID, Type: constant.ChannelTypeGemini, Key: "channel-key", BaseURL: common.GetPointer(baseURL),
			}).Error)
			require.NoError(t, model.DB.Create(&model.Task{
				TaskID: "task_gemini_secret_" + fmt.Sprint(index), UserId: 1, ChannelId: channelID,
				Status: model.TaskStatusSuccess,
				Data:   []byte(fmt.Sprintf(`{"uri":%q}`, videoURL)),
				PrivateData: model.TaskPrivateData{
					Key: apiKey,
				},
			}).Error)

			client := service.GetSSRFProtectedHTTPClient()
			originalTransport := client.Transport
			requestedURL := ""
			requestedKey := ""
			client.Transport = videoProxyTestRoundTripper(func(request *http.Request) (*http.Response, error) {
				requestedURL = request.URL.String()
				requestedKey = request.Header.Get("x-goog-api-key")
				return test.transport(request)
			})
			t.Cleanup(func() { client.Transport = originalTransport })

			var logs bytes.Buffer
			common.LogWriterMu.Lock()
			originalErrorWriter := gin.DefaultErrorWriter
			gin.DefaultErrorWriter = &logs
			common.LogWriterMu.Unlock()
			t.Cleanup(func() {
				common.LogWriterMu.Lock()
				gin.DefaultErrorWriter = originalErrorWriter
				common.LogWriterMu.Unlock()
			})

			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set("id", 1)
				c.Next()
			})
			router.GET("/v1/videos/:task_id/content", VideoProxy)

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/v1/videos/task_gemini_secret_"+fmt.Sprint(index)+"/content", nil)
			router.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusBadGateway, recorder.Code)
			assert.Equal(t, apiKey, requestedKey)
			assert.NotContains(t, requestedURL, apiKey)
			assert.Contains(t, requestedURL, "alt=media")
			assert.NotContains(t, logs.String(), apiKey)
			assert.NotContains(t, recorder.Body.String(), apiKey)
		})
	}
}

func TestRedactVideoProxySensitiveQuery(t *testing.T) {
	raw := "request failed for https://provider.example/video?key=secret-key&alt=media and https://backup.example/video?ACCESS_TOKEN=secret-token&X-Goog-Signature=public-signature"

	redacted := redactVideoProxySensitiveQuery(raw)

	assert.NotContains(t, redacted, "secret-key")
	assert.NotContains(t, redacted, "secret-token")
	assert.Contains(t, redacted, "key=[REDACTED]")
	assert.Contains(t, redacted, "ACCESS_TOKEN=[REDACTED]")
	assert.Contains(t, redacted, "alt=media")
	assert.Contains(t, redacted, "X-Goog-Signature=public-signature")
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
