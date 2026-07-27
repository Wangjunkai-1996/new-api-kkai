package jimeng

import (
	"io"
	"net/http"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoResponseRequiresExplicitJimengAcceptance(t *testing.T) {
	tests := []struct {
		name               string
		body               string
		wantAccepted       bool
		wantSubmissionOpen bool
	}{
		{name: "missing code", body: `{}`, wantSubmissionOpen: true},
		{name: "missing code with task", body: `{"data":{"task_id":"upstream-task"}}`, wantSubmissionOpen: true},
		{name: "explicit rejection", body: `{"code":10001,"message":"rejected"}`},
		{name: "success without task id", body: `{"code":10000,"message":"ok"}`, wantSubmissionOpen: true},
		{name: "explicit success", body: `{"code":10000,"message":"ok","data":{"task_id":"upstream-task"}}`, wantAccepted: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adaptor := &TaskAdaptor{}
			response, responseErr := adaptor.DoResponse(nil, &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(test.body)),
			}, &relaycommon.RelayInfo{
				OriginModelName: "jimeng-test",
				TaskRelayInfo: &relaycommon.TaskRelayInfo{
					PublicTaskID: "task_public",
				},
			})

			if !test.wantAccepted {
				require.Nil(t, response)
				require.NotNil(t, responseErr)
				assert.Equal(t, test.wantSubmissionOpen, responseErr.SubmissionPossible())
				return
			}

			require.Nil(t, responseErr)
			require.NotNil(t, response)
			assert.Equal(t, "upstream-task", response.UpstreamTaskID)
			assert.JSONEq(t, test.body, string(response.TaskData))
		})
	}
}
