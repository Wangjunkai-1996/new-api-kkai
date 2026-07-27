package suno

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoResponseRequiresExplicitSunoAcceptance(t *testing.T) {
	tests := []struct {
		name               string
		body               string
		wantAccepted       bool
		wantSubmissionOpen bool
	}{
		{name: "missing code", body: `{}`, wantSubmissionOpen: true},
		{name: "missing code with task", body: `{"data":"upstream-task"}`, wantSubmissionOpen: true},
		{name: "explicit rejection", body: `{"code":"error","message":"rejected"}`},
		{name: "success without task id", body: `{"code":"success","message":"ok"}`, wantSubmissionOpen: true},
		{name: "explicit success", body: `{"code":"success","message":"ok","data":"upstream-task"}`, wantAccepted: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adaptor := &TaskAdaptor{}
			response, responseErr := adaptor.DoResponse(nil, &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(test.body)),
			}, &relaycommon.RelayInfo{
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

			var publicResponse dto.TaskResponse[string]
			require.NoError(t, common.Unmarshal(response.Body, &publicResponse))
			assert.Equal(t, "task_public", publicResponse.Data)
		})
	}
}
