package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantRuntimeRequiresSocketLoopbackAndFreezesSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	snapshot := service.TenantRuntimeSnapshot{Protocol: "tenant-online-v1", Version: "bridge-at-startup", SchemaAtStartup: "expanded"}
	engine := gin.New()
	engine.GET("/runtime", TenantRuntime(snapshot))
	// A later change of the source value cannot change a running process proof.
	snapshot.Version = "different"
	snapshot.SchemaAtStartup = "finalized"
	for _, remote := range []string{"127.0.0.1:31000", "[::1]:31000", "172.18.0.4:31000", "203.0.113.1:31000", "127.0.0.1", "invalid:31000"} {
		t.Run(remote, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/runtime", nil)
			request.RemoteAddr = remote
			request.Header.Set("X-Forwarded-For", "127.0.0.1")
			request.Header.Set("X-Real-IP", "127.0.0.1")
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
			if remote != "127.0.0.1:31000" && remote != "[::1]:31000" {
				assert.Equal(t, http.StatusNotFound, response.Code)
				assert.Empty(t, response.Body.String())
				return
			}
			require.Equal(t, http.StatusOK, response.Code)
			var result struct {
				Success bool `json:"success"`
				Data    struct {
					Runtime service.TenantRuntimeSnapshot `json:"tenant_runtime"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
			assert.True(t, result.Success)
			assert.Equal(t, "bridge-at-startup", result.Data.Runtime.Version)
			assert.Equal(t, "expanded", result.Data.Runtime.SchemaAtStartup)
			assert.False(t, result.Data.Runtime.ResellerReadyAtStartup)
		})
	}
}
