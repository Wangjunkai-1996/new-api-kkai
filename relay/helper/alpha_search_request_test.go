package helper

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAlphaSearchValidationPreservesProviderFields(t *testing.T) {
	for _, body := range []string{`{"query":"test"}`, `{"model":"alpha","query":"test","provider_options":{"limit":0}}`} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/alpha/search", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		t.Cleanup(func() { common.CleanupBodyStorage(c) })
		request, err := GetAndValidateRequest(c, types.RelayFormatOpenAIAlphaSearch)
		if !strings.Contains(body, `"model"`) {
			require.EqualError(t, err, "model is required")
			continue
		}
		require.NoError(t, err)
		search, ok := request.(*dto.AlphaSearchRequest)
		require.True(t, ok)
		assert.Equal(t, "alpha", search.Model)
		assert.Equal(t, body, string(search.RawBody))
	}
}
