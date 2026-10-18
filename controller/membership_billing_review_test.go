package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestMembershipBillingReviewRequiresExplicitUsageAndAuditIdentity(t *testing.T) {
	for _, tc := range []struct {
		name  string
		actor int
		body  string
	}{
		{"omitted_usage", 1, `{"operation_id":"audit","action":"settle","reason":"verified"}`},
		{"null_usage", 1, `{"operation_id":"audit","action":"settle","actual_quota":null,"reason":"verified"}`},
		{"negative_usage", 1, `{"operation_id":"audit","action":"settle","actual_quota":-1,"reason":"verified"}`},
		{"release_with_charge", 1, `{"operation_id":"audit","action":"release","actual_quota":1,"reason":"verified"}`},
		{"no_actor", 0, `{"operation_id":"audit","action":"settle","actual_quota":0,"reason":"verified"}`},
		{"no_evidence", 1, `{"operation_id":"audit","action":"settle","actual_quota":0,"reason":" "}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			router.POST("/review/:request_id", func(c *gin.Context) { c.Set("id", tc.actor); c.Next() }, AdminResolveMembershipBillingReview)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/review/request", strings.NewReader(tc.body)))
			assert.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.Contains(t, recorder.Body.String(), "membership_invalid_request")
		})
	}
}
