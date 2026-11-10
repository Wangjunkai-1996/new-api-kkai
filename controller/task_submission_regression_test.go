package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type taskSubmissionBillingSpy struct{ refunds int }

func (s *taskSubmissionBillingSpy) Settle(int) error              { return nil }
func (s *taskSubmissionBillingSpy) Refund(*gin.Context)           { s.refunds++ }
func (s *taskSubmissionBillingSpy) NeedsRefund() bool             { return true }
func (s *taskSubmissionBillingSpy) GetPreConsumedQuota() int      { return 10 }
func (s *taskSubmissionBillingSpy) Reserve(int) error             { return nil }
func (s *taskSubmissionBillingSpy) ReserveBeforeSubmit(int) error { return nil }

func TestNativeTaskStartedResponseIsNotRewrittenOrRefunded(t *testing.T) {
	previousRetries := common.RetryTimes
	common.RetryTimes = -1 // No dispatch: exercise only the failed-submit cleanup boundary.
	t.Cleanup(func() { common.RetryTimes = previousRetries })
	for _, written := range []bool{false, true} {
		t.Run(map[bool]string{false: "unwritten", true: "started"}[written], func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/native/render", nil)
			if written {
				ctx.String(http.StatusOK, "data: initial\n\n")
				ctx.Writer.Flush()
			}
			billing := &taskSubmissionBillingSpy{}
			_, taskErr := executeTaskSubmission(ctx, &relaycommon.RelayInfo{Billing: billing})
			require.NotNil(t, taskErr)
			if written {
				assert.Zero(t, billing.refunds)
				presentTaskSubmission(ctx, nil)
				respondTaskSubmissionError(ctx, &dto.TaskError{StatusCode: 500, Message: "private upstream credential"})
				assert.Equal(t, "data: initial\n\n", recorder.Body.String())
				assert.Equal(t, http.StatusOK, recorder.Code)
				decision := decideTaskRetry(ctx, &dto.TaskError{StatusCode: 503}, 2)
				assert.Equal(t, "stop", decision.Action)
			} else {
				assert.Equal(t, 1, billing.refunds)
			}
		})
	}
}
