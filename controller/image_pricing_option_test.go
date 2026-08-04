package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/setting/image_pricing_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImagePricingPolicyHashOptionIsReadOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/option",
		bytes.NewBufferString(`{"key":"ImagePricingPolicyHash","value":"forged"}`),
	)
	context.Request.Header.Set("Content-Type", "application/json")
	before := image_pricing_setting.PolicyHash()

	UpdateOption(context)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":false`)
	assert.Equal(t, before, image_pricing_setting.PolicyHash())
}
