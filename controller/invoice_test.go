package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvoiceHandlersRequireTrustedSessionHostAndAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name    string
		session bool
		tenant  *int64
		role    int
		admin   bool
		code    string
	}{
		{"no-session", false, new(int64), common.RoleAdminUser, true, "SESSION_REQUIRED"},
		{"missing-host", true, nil, common.RoleAdminUser, true, "FORBIDDEN"},
		{"tenant-host", true, func() *int64 { n := int64(42); return &n }(), common.RoleAdminUser, true, "FORBIDDEN"},
		{"non-admin", true, new(int64), common.RoleCommonUser, true, "FORBIDDEN"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(response)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/api/invoices/private", nil)
			ctx.Set("id", 7)
			ctx.Set("role", test.role)
			if test.session {
				ctx.Set("session_id", "dashboard-session")
				ctx.Set("auth_version", int64(1))
				ctx.Set("session_version", int64(1))
			}
			if test.tenant != nil {
				common.SetContextKey(ctx, constant.ContextKeyTenantContext, model.TenantContext{TenantID: *test.tenant})
			}
			InvoiceDetail(test.admin)(ctx)
			assert.Equal(t, http.StatusForbidden, response.Code)
			assert.Contains(t, response.Body.String(), test.code)
			if test.name != "non-admin" {
				verifyResponse := httptest.NewRecorder()
				verifyContext, _ := gin.CreateTestContext(verifyResponse)
				verifyContext.Request, verifyContext.Keys = ctx.Request, ctx.Keys
				InvoiceVerifyPayment(verifyContext)
				assert.Equal(t, http.StatusForbidden, verifyResponse.Code)
				assert.Contains(t, verifyResponse.Body.String(), test.code)
			}
		})
	}
}

func TestInvoiceJSONBoundaryRejectsTrailingAndOversizedBodies(t *testing.T) {
	for _, test := range []struct {
		body   string
		status int
	}{
		{`{} {}`, http.StatusBadRequest},
		{`{"note":"` + strings.Repeat("x", 65<<10) + `"}`, http.StatusRequestEntityTooLarge},
		{`{"note":"valid"}`, http.StatusOK},
	} {
		response := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(response)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/invoices", strings.NewReader(test.body))
		var input struct {
			Note string `json:"note"`
		}
		ok := invoiceDecode(ctx, &input)
		assert.Equal(t, test.status == http.StatusOK, ok)
		if !ok {
			require.Equal(t, test.status, response.Code, response.Body.String())
		}
	}
}
