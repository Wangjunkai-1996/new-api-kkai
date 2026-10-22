package middleware

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRequestTicketSchemaGateFailsClosedAndFreezesReadiness(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var queries bytes.Buffer
	db, err := gorm.Open(sqlite.Open("file:"+t.TempDir()+"/request-schema.db"), &gorm.Config{
		Logger: logger.New(log.New(&queries, "", 0), logger.Config{LogLevel: logger.Info}),
	})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	missingGate := RequestTicketSchemaGate(db)
	// The startup result remains closed even if someone adds a claimed version
	// later. A new gate independently rejects the forged canonical receipt.
	require.NoError(t, db.Exec("CREATE TABLE kkai_schema_migrations(version BIGINT PRIMARY KEY,name TEXT,checksum TEXT,applied_at BIGINT,execution_ms BIGINT)").Error)
	require.NoError(t, db.Exec("INSERT INTO kkai_schema_migrations(version,name,checksum) VALUES(16,'request_tickets_and_notifications','forged')").Error)
	for _, test := range []struct {
		name string
		gate gin.HandlerFunc
	}{
		{name: "nil database", gate: RequestTicketSchemaGate(nil)},
		{name: "missing receipt at startup", gate: missingGate},
		{name: "forged receipt at startup", gate: RequestTicketSchemaGate(db)},
	} {
		t.Run(test.name, func(t *testing.T) {
			engine := gin.New()
			engine.GET("/api/requests", test.gate, func(c *gin.Context) {
				t.Fatal("request ticket handler reached an unverified database")
			})
			engine.GET("/api/status", func(c *gin.Context) { c.Status(http.StatusOK) })
			queries.Reset()
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/requests", nil))
			assert.Equal(t, http.StatusServiceUnavailable, response.Code)
			assert.JSONEq(t, `{"success":false,"code":"REQUEST_TICKET_SCHEMA_NOT_READY","message":"request tickets are not enabled"}`, response.Body.String())
			assert.Empty(t, queries.String(), "requests must use the frozen startup result without database probes")
			status := httptest.NewRecorder()
			engine.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/api/status", nil))
			assert.Equal(t, http.StatusOK, status.Code)
		})
	}
}
