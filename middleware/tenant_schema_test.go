package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTenantSchemaGateRejectsMissingOrForgedFinalSchema(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+t.TempDir()+"/schema.db"), &gorm.Config{})
	require.NoError(t, err)
	for _, withForgedVersion := range []bool{false, true} {
		if withForgedVersion {
			require.NoError(t, db.Exec("CREATE TABLE kkai_schema_migrations(version BIGINT PRIMARY KEY,name TEXT,checksum TEXT,applied_at BIGINT,execution_ms BIGINT)").Error)
			require.NoError(t, db.Exec("INSERT INTO kkai_schema_migrations(version,name,checksum) VALUES(14,'forged','forged')").Error)
		}
		engine := gin.New()
		engine.POST("/api/reseller/applications", TenantBusinessSchemaGate(db), func(c *gin.Context) {
			t.Error("reseller mutation reached an incomplete database")
			c.Status(http.StatusOK)
		})
		engine.GET("/api/status", func(c *gin.Context) { c.Status(http.StatusOK) })
		for _, test := range []struct {
			method, path string
			status       int
		}{{http.MethodPost, "/api/reseller/applications", http.StatusServiceUnavailable}, {http.MethodGet, "/api/status", http.StatusOK}} {
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
			assert.Equal(t, test.status, response.Code)
		}
	}
}
