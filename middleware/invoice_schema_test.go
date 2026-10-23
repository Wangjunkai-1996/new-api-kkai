package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvoiceSchemaGateEnablesOnlyAfterReceiptAndRechecksDrift(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Unix(100, 0)
	ready := false
	checks := 0
	router := gin.New()
	router.GET("/invoices", invoiceSchemaGate(func(context.Context) error {
		checks++
		if !ready {
			return errors.New("missing canonical receipt")
		}
		return nil
	}, func() time.Time { return now }), func(c *gin.Context) { c.Status(http.StatusOK) })
	request := func() *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/invoices", nil))
		return response
	}
	response := request()
	assert.Equal(t, http.StatusServiceUnavailable, response.Code)
	assert.Contains(t, response.Body.String(), "SCHEMA_NOT_READY")
	ready = true
	assert.Equal(t, http.StatusServiceUnavailable, request().Code, "failed checks are throttled")
	assert.Equal(t, 1, checks)
	now = now.Add(31 * time.Second)
	assert.Equal(t, http.StatusOK, request().Code)
	ready = false
	now = now.Add(31 * time.Second)
	assert.Equal(t, http.StatusServiceUnavailable, request().Code, "successful checks must eventually revalidate physical drift")
	require.Equal(t, 3, checks)
}
