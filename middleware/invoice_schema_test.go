package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvoiceSchemaGate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name     string
		ready    bool
		canceled bool
		status   int
	}{
		{name: "ready", ready: true, status: http.StatusOK},
		{name: "not ready", status: http.StatusServiceUnavailable},
		{name: "canceled request", ready: true, canceled: true, status: http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.canceled {
				cancel()
			}
			request := httptest.NewRequest(http.MethodGet, "/invoices", nil).WithContext(ctx)
			checked, handled := false, false
			router := gin.New()
			router.GET("/invoices", invoiceSchemaGate(func(received context.Context) bool {
				checked = true
				require.Same(t, ctx, received)
				return tc.ready && received.Err() == nil
			}), func(c *gin.Context) {
				handled = true
				c.Status(http.StatusOK)
			})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.True(t, checked)
			assert.Equal(t, tc.status, response.Code)
			assert.Equal(t, tc.status == http.StatusOK, handled)
			if tc.status == http.StatusServiceUnavailable {
				assert.JSONEq(t, `{"success":false,"code":"SCHEMA_NOT_READY","message":"invoice center is not enabled"}`, response.Body.String())
			}
		})
	}
}
