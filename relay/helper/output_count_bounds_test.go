package helper

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOutputCountBounds(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name  string
		value string
		valid bool
	}{
		{"omitted", "", true},
		{"null", "null", true},
		{"zero", "0", true},
		{"negative zero", "-0", true},
		{"upper bound", "128", true},
		{"negative", "-1", false},
		{"over upper bound", "129", false},
		{"integer overflow", "18446744073686646784", false},
		{"fraction", "1.5", false},
		{"string", `"1"`, false},
	} {
		for _, format := range []string{"chat", "gemini", "gemini snake count", "gemini batch"} {
			t.Run(format+"/"+test.name, func(t *testing.T) {
				field := ""
				body := `{"model":"test","messages":[{"role":"user","content":"hello"}]%s}`
				switch format {
				case "chat":
					if test.value != "" {
						field = `,"n":` + test.value
					}
				case "gemini", "gemini snake count", "gemini batch":
					key := "candidateCount"
					if format == "gemini snake count" {
						key = "candidate_count"
					}
					if test.value != "" {
						field = fmt.Sprintf(`%q:%s`, key, test.value)
					}
					body = `{"contents":[{"parts":[{"text":"hello"}]}],"generationConfig":{%s}}`
				}
				body = fmt.Sprintf(body, field)
				if format == "gemini batch" {
					body = `{"requests":[` + body + `]}`
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/relay", strings.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")
				t.Cleanup(func() { common.CleanupBodyStorage(c) })
				var err error
				if format == "chat" {
					_, err = GetAndValidateTextRequest(c, relayconstant.RelayModeChatCompletions)
				} else {
					_, err = GetAndValidateGeminiRequest(c)
				}
				if test.valid {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			})
		}
	}
	t.Run("batch max tokens bounded", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/relay", strings.NewReader(`{"requests":[{"contents":[{"parts":[{"text":"hello"}]}],"generationConfig":{"maxOutputTokens":18446744073686646784}}]}`))
		c.Request.Header.Set("Content-Type", "application/json")
		t.Cleanup(func() { common.CleanupBodyStorage(c) })
		_, err := GetAndValidateGeminiRequest(c)
		require.ErrorContains(t, err, "maxOutputTokens is invalid")
	})
}
