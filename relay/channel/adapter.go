package channel

import (
	"errors"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type Adaptor interface {
	// Init IsStream bool
	Init(info *relaycommon.RelayInfo)
	GetRequestURL(info *relaycommon.RelayInfo) (string, error)
	SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error
	ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error)
	ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error)
	ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error)
	ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error)
	ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error)
	ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error)
	DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error)
	DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError)
	GetModelList() []string
	GetChannelName() string
	ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error)
	ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error)
}

type TaskAdaptor interface {
	Init(info *relaycommon.RelayInfo)

	ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError

	// ── Billing ──────────────────────────────────────────────────────

	// EstimateBilling returns OtherRatios for pre-charge based on user request.
	// Called after ValidateRequestAndSetAction, before price calculation.
	// Adaptors should extract duration, resolution, etc. from the parsed request
	// and return them as ratio multipliers (e.g. {"seconds": 5, "size": 1.666}).
	// Return nil to use the base model price without extra ratios.
	EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64

	// AdjustBillingOnSubmit returns adjusted OtherRatios from the upstream
	// submit response. Called after a successful ParseResponse.
	// If the upstream returned actual parameters that differ from the estimate
	// (e.g. actual seconds), return updated ratios so the caller can recalculate
	// the quota and settle the delta with the pre-charge.
	// Return nil if no adjustment is needed.
	AdjustBillingOnSubmit(info *relaycommon.RelayInfo, taskData []byte) map[string]float64

	// AdjustBillingOnComplete returns the actual quota when a task reaches a
	// terminal state (success/failure) during polling.
	// Called by the polling loop after ParseTaskResult.
	// Return a positive value to trigger delta settlement (supplement / refund).
	// Return 0 to keep the pre-charged amount unchanged.
	AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int

	// ── Request / Response ───────────────────────────────────────────

	BuildRequestURL(info *relaycommon.RelayInfo) (string, error)
	BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error
	BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error)

	DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error)
	ParseResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*TaskSubmitResponse, *dto.TaskError)

	GetModelList() []string
	GetChannelName() string

	// ── Polling ──────────────────────────────────────────────────────

	FetchTask(baseUrl, key string, task *model.Task, proxy string) (*http.Response, error)
	ParseTaskResult(task *model.Task, resp *http.Response, respBody []byte) (*relaycommon.TaskInfo, error)
}

// TaskSubmitResponse is the transport-independent result of parsing an
// upstream task submission. Parsing must not write to the client response.
type TaskSubmitResponse struct {
	UpstreamTaskID string
	TaskData       []byte
	ClientResponse any
	Immediate      *relaycommon.TaskInfo
	PluginState    []byte
	// Legacy buffered response fields are retained for task adaptor tests and
	// compatibility helpers. New adaptors should prefer ClientResponse.
	StatusCode  int
	ContentType string
	Body        []byte
}

// TaskResponseError preserves the pre-rc.41 distinction between a rejected
// request and a transport failure where the upstream may have accepted it.
// The current TaskAdaptor contract returns *dto.TaskError directly; callers
// that still use this helper can migrate without changing retry semantics.
type TaskResponseError struct {
	TaskError          *dto.TaskError
	submissionPossible bool
}

func NewRejectedTaskResponseError(taskErr *dto.TaskError) *TaskResponseError {
	return &TaskResponseError{TaskError: taskErr}
}

func NewUncertainTaskResponseError(taskErr *dto.TaskError) *TaskResponseError {
	return &TaskResponseError{TaskError: taskErr, submissionPossible: true}
}

func (e *TaskResponseError) SubmissionPossible() bool {
	return e != nil && e.submissionPossible
}

func NewJSONTaskSubmitResponse(upstreamTaskID string, taskData []byte, clientBody any) (*TaskSubmitResponse, error) {
	body, err := common.Marshal(clientBody)
	if err != nil {
		return nil, err
	}
	return &TaskSubmitResponse{
		UpstreamTaskID: upstreamTaskID,
		TaskData:       taskData,
		StatusCode:     http.StatusOK,
		ContentType:    "application/json; charset=utf-8",
		Body:           body,
	}, nil
}

func (r *TaskSubmitResponse) WriteTo(c *gin.Context) error {
	if r == nil {
		return errors.New("task submit response is nil")
	}
	if c == nil || c.Writer == nil {
		return errors.New("task submit response writer is nil")
	}
	statusCode := r.StatusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	contentType := r.ContentType
	if contentType == "" {
		contentType = "application/json; charset=utf-8"
	}
	c.Header("Content-Type", contentType)
	c.Status(statusCode)
	_, err := c.Writer.Write(r.Body)
	return err
}

type OpenAIVideoConverter interface {
	ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error)
}

type TaskArtifact = types.TaskArtifact

type TaskArtifactClientRequest struct {
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers,omitempty"`
}

type TaskArtifactProvider interface {
	ListArtifacts(task *model.Task) ([]TaskArtifact, error)
}

type TaskContentRequest struct {
	URL            string
	Method         string
	Headers        map[string]string
	Body           []byte
	Credentialless bool
}

type TaskContentRequestProvider interface {
	BuildContentRequest(task *model.Task, artifactKey string, clientRequest TaskArtifactClientRequest) (*TaskContentRequest, error)
}

// TaskRequestError records whether an upstream request may have been written.
// Callers must not retry when a transport failure occurred after dispatch.
type TaskRequestError struct {
	Err                error
	submissionPossible bool
}

func NewTaskRequestError(err error, submissionPossible bool) *TaskRequestError {
	return &TaskRequestError{Err: err, submissionPossible: submissionPossible}
}

func (e *TaskRequestError) Error() string {
	if e == nil || e.Err == nil {
		return "task request failed"
	}
	return e.Err.Error()
}

func (e *TaskRequestError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *TaskRequestError) SubmissionPossible() bool {
	return e != nil && e.submissionPossible
}

type TaskUsageFactsProvider interface {
	ExtractUsageFacts(c *gin.Context, info *relaycommon.RelayInfo) map[string]any
}

// TaskValidatedBillingProvider lets an adaptor reject invalid usage facts at
// the existing estimate point, after model mapping and before quota
// multiplication. Non-plugin task adaptors keep using EstimateBilling.
type TaskValidatedBillingProvider interface {
	EstimateBillingValidated(c *gin.Context, info *relaycommon.RelayInfo) (map[string]float64, error)
}

// TaskValidatedUsageFactsProvider is the tiered-billing counterpart to
// TaskValidatedBillingProvider.
type TaskValidatedUsageFactsProvider interface {
	ExtractUsageFactsValidated(c *gin.Context, info *relaycommon.RelayInfo) (map[string]any, error)
}
