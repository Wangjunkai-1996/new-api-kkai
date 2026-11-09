package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestMembershipRecoveryObservationRejectsForwardedLoopback(t *testing.T) {
	for _, peer := range []string{"192.0.2.3:1234", "invalid"} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/api/internal/membership-recovery", nil)
		ctx.Request.RemoteAddr = peer
		ctx.Request.Header.Set("X-Forwarded-For", "127.0.0.1")
		MembershipBillingRecoveryRuntime(ctx)
		assert.Equal(t, http.StatusNotFound, recorder.Code)
	}
}

func TestMembershipRecoveryControlCannotUseGenericOptionsAPI(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/option/", strings.NewReader(`{"key":"MembershipBillingRecoveryControl","value":"apply:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`))
	UpdateOption(ctx)
	assert.Equal(t, http.StatusForbidden, recorder.Code)
}
