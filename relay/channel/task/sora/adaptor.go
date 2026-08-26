package sora

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/tidwall/sjson"
)

// ============================
// Request / Response structures
// ============================

type ContentItem struct {
	Type     string    `json:"type"`                // "text" or "image_url"
	Text     string    `json:"text,omitempty"`      // for text type
	ImageURL *ImageURL `json:"image_url,omitempty"` // for image_url type
}

type ImageURL struct {
	URL string `json:"url"`
}

type responseTask struct {
	ID                 string `json:"id"`
	TaskID             string `json:"task_id,omitempty"` //兼容旧接口
	Object             string `json:"object"`
	Model              string `json:"model"`
	Status             string `json:"status"`
	Progress           int    `json:"progress"`
	CreatedAt          int64  `json:"created_at"`
	CompletedAt        int64  `json:"completed_at,omitempty"`
	ExpiresAt          int64  `json:"expires_at,omitempty"`
	Seconds            string `json:"seconds,omitempty"`
	Size               string `json:"size,omitempty"`
	RemixedFromVideoID string `json:"remixed_from_video_id,omitempty"`
	Error              *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
}

// ============================
// Adaptor implementation
// ============================

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
}

func validateRemixRequest(c *gin.Context) *dto.TaskError {
	var req relaycommon.TaskSubmitReq
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("field prompt is required"), "invalid_request", http.StatusBadRequest)
	}
	// 存储原始请求到 context，与 ValidateMultipartDirect 路径保持一致
	c.Set("task_request", req)
	return nil
}

// seedanceDurationBounds is intentionally an explicit capability table. A
// broad prefix/substring match could apply Seedance validation to an unrelated
// customer alias that happens to contain "sd_2.0" or "seedance-2.5".
var seedanceDurationBounds = map[string][2]int{
	"sd_2.0_fast_special_720p":                {4, 15},
	"sd_2.0_special_720p":                     {4, 15},
	"sd_2.0_special_1080p":                    {4, 15},
	"sd_2.0_special_2k":                       {4, 15},
	"sd_2.0_special_4k":                       {4, 15},
	"sd_2.0_fast_special_720p_with_video_ref": {4, 15},
	"sd_2.0_special_720p_with_video_ref":      {4, 15},
	"sd_2.0_special_1080p_with_video_ref":     {4, 15},
	"sd_2.0_special_2k_with_video_ref":        {4, 15},
	"sd_2.0_special_4k_with_video_ref":        {4, 15},
	"seedance-2.5":                            {4, 30},
}

func durationBoundsForModel(model string) (int, int, bool) {
	bounds, ok := seedanceDurationBounds[strings.ToLower(strings.TrimSpace(model))]
	if !ok {
		return 0, 0, false
	}
	return bounds[0], bounds[1], true
}

func isSeedanceSpecialModel(model string) bool {
	_, _, ok := durationBoundsForModel(model)
	return ok
}

func requestDurationModel(c *gin.Context, req relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) string {
	candidates := make([]string, 0, 4)
	if info != nil {
		candidates = append(candidates, info.UpstreamModelName, info.OriginModelName)
		if info.ChannelMeta != nil {
			candidates = append(candidates, info.ChannelMeta.UpstreamModelName)
		}
	}
	candidates = append(candidates, req.Model)

	// Model mapping is applied after adaptor validation. Resolve the same
	// configured chain here so a custom public alias still receives the
	// model-specific duration contract before billing or dispatch.
	var mapping map[string]string
	if c != nil {
		mappingJSON := strings.TrimSpace(c.GetString("model_mapping"))
		if mappingJSON != "" && mappingJSON != "{}" {
			_ = common.UnmarshalJsonStr(mappingJSON, &mapping)
		}
	}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		current := candidate
		visited := map[string]struct{}{current: {}}
		for step := 0; step < 32; step++ {
			if _, _, supported := durationBoundsForModel(current); supported {
				return current
			}
			next, ok := mapping[current]
			if !ok || strings.TrimSpace(next) == "" {
				break
			}
			next = strings.TrimSpace(next)
			if _, seen := visited[next]; seen {
				break
			}
			visited[next] = struct{}{}
			current = next
		}
		if _, _, supported := durationBoundsForModel(current); supported {
			return current
		}
	}
	for _, candidate := range candidates {
		if candidate = strings.TrimSpace(candidate); candidate != "" {
			return candidate
		}
	}
	return ""
}

func mergeVideoStudioMetadata(c *gin.Context, req relaycommon.TaskSubmitReq) (relaycommon.TaskSubmitReq, error) {
	if c == nil || c.Request == nil || strings.TrimRight(c.Request.URL.Path, "/") != "/pg/videos" {
		return req, nil
	}
	var metadataReq relaycommon.TaskSubmitReq
	if err := req.UnmarshalMetadata(&metadataReq); err != nil {
		return req, errors.Wrap(err, "invalid video studio metadata")
	}
	return relaycommon.MergeTaskDurationFields(req, metadataReq), nil
}

func requestModelHint(c *gin.Context, info *relaycommon.RelayInfo) string {
	model := requestDurationModel(c, relaycommon.TaskSubmitReq{}, info)
	if model != "" {
		return model
	}
	if c != nil && c.Request != nil && strings.HasPrefix(strings.ToLower(c.GetHeader("Content-Type")), "multipart/form-data") {
		// The channel model is normally present in RelayInfo. FormValue is only a
		// fallback for direct adapter tests or a malformed context.
		return strings.TrimSpace(c.Request.FormValue("model"))
	}
	return ""
}

func seedanceDurationTaskError(field string, min, max int) *dto.TaskError {
	return service.TaskErrorWrapperLocal(
		fmt.Errorf("%s: must be an integer between %d and %d", field, min, max),
		"invalid_duration", http.StatusBadRequest,
	)
}

// parseRawSeedanceDurationField validates the wire representation before
// TaskSubmitReq's typed decoder runs. The typed decoder intentionally keeps
// strict semantics for other task providers, but Seedance needs a stable
// field-specific error for malformed values instead of generic invalid_json.
func parseRawSeedanceDurationField(raw json.RawMessage) (int, bool, error) {
	switch common.GetJsonType(raw) {
	case "null":
		return 0, false, nil
	case "number":
		value, err := strconv.Atoi(strings.TrimSpace(string(raw)))
		if err != nil {
			return 0, false, err
		}
		return value, true, nil
	case "string":
		var value string
		if err := common.Unmarshal(raw, &value); err != nil {
			return 0, false, err
		}
		if value == "" {
			return 0, false, nil
		}
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return 0, false, err
		}
		if strconv.Itoa(parsed) != value {
			return 0, false, fmt.Errorf("value must be a canonical integer string")
		}
		return parsed, true, nil
	default:
		return 0, false, fmt.Errorf("value must be an integer number or integer string")
	}
}

func decodeSeedanceMetadataFields(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || common.GetJsonType(raw) == "null" {
		return nil, nil
	}
	if common.GetJsonType(raw) == "string" {
		var metadataString string
		if err := common.Unmarshal(raw, &metadataString); err != nil {
			return nil, err
		}
		if strings.TrimSpace(metadataString) == "" {
			return nil, nil
		}
		raw = json.RawMessage(metadataString)
	}
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(raw, &fields); err != nil || fields == nil {
		if err == nil {
			err = fmt.Errorf("metadata must be a JSON object")
		}
		return nil, err
	}
	return fields, nil
}

// validateSeedanceRawDuration performs the model-specific duration check
// before the shared task validator decodes the request into typed fields.
// This preserves the public error contract for values such as 5.5 or "abc".
func validateSeedanceRawDuration(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if c == nil || c.Request == nil {
		return nil
	}
	contentType := strings.ToLower(strings.TrimSpace(c.GetHeader("Content-Type")))
	if !strings.HasPrefix(contentType, "application/json") {
		return nil
	}

	var fields map[string]json.RawMessage
	if err := common.UnmarshalBodyReusable(c, &fields); err != nil || fields == nil {
		// Let the shared validator return its normal invalid_json response for
		// malformed/non-object requests; this helper only owns duration errors.
		return nil
	}

	// In direct tests the channel metadata may be absent. Resolve the model
	// from the raw request as a fallback while retaining configured mappings.
	model := requestDurationModel(c, relaycommon.TaskSubmitReq{}, info)
	if model == "" {
		if rawModel, ok := fields["model"]; ok && common.GetJsonType(rawModel) == "string" {
			var requestModel string
			if common.Unmarshal(rawModel, &requestModel) == nil {
				model = requestDurationModel(c, relaycommon.TaskSubmitReq{Model: requestModel}, info)
			}
		}
	}
	min, max, supported := durationBoundsForModel(model)
	if !supported {
		return nil
	}

	checkFields := func(values map[string]json.RawMessage) *dto.TaskError {
		for _, field := range []string{"duration", "seconds"} {
			raw, present := values[field]
			if !present {
				continue
			}
			value, _, parseErr := parseRawSeedanceDurationField(raw)
			if parseErr != nil || value < 0 || value > max {
				return seedanceDurationTaskError(field, min, max)
			}
		}
		return nil
	}
	if taskErr := checkFields(fields); taskErr != nil {
		return taskErr
	}

	// Video Studio stores configured fields in metadata. Only inspect metadata
	// on that internal route; the public API keeps duration at the top level.
	if strings.TrimRight(c.Request.URL.Path, "/") == "/pg/videos" {
		if rawMetadata, exists := fields["metadata"]; exists {
			metadataFields, metadataErr := decodeSeedanceMetadataFields(rawMetadata)
			if metadataErr != nil {
				return service.TaskErrorWrapperLocal(metadataErr, "invalid_metadata", http.StatusBadRequest)
			}
			if taskErr := checkFields(metadataFields); taskErr != nil {
				return taskErr
			}
		}
	}
	return nil
}

func rejectSeedanceSpecialTransport(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if c == nil {
		return nil
	}
	if !isSeedanceSpecialModel(requestModelHint(c, info)) {
		return nil
	}
	contentType := strings.ToLower(strings.TrimSpace(c.GetHeader("Content-Type")))
	if !strings.HasPrefix(contentType, "application/json") {
		return service.TaskErrorWrapperLocal(
			fmt.Errorf("Seedance special models accept application/json requests only"),
			"invalid_content_type", http.StatusBadRequest,
		)
	}
	return nil
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *dto.TaskError) {
	if info.Action == constant.TaskActionRemix {
		if isSeedanceSpecialModel(requestModelHint(c, info)) {
			return service.TaskErrorWrapperLocal(
				fmt.Errorf("Seedance special models do not support remix"),
				"unsupported_operation", http.StatusBadRequest,
			)
		}
		return validateRemixRequest(c)
	}
	if taskErr := rejectSeedanceSpecialTransport(c, info); taskErr != nil {
		return taskErr
	}
	if taskErr := validateSeedanceRawDuration(c, info); taskErr != nil {
		return taskErr
	}
	if taskErr := relaycommon.ValidateMultipartDirect(c, info); taskErr != nil {
		return taskErr
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	model := requestDurationModel(c, req, info)
	min, max, supported := durationBoundsForModel(model)
	if !supported {
		return nil
	}
	req, err = mergeVideoStudioMetadata(c, req)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_metadata", http.StatusBadRequest)
	}
	normalized, _, err := relaycommon.NormalizeTaskDuration(req, min, max)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_duration", http.StatusBadRequest)
	}
	// Keep the canonical request in context so billing and request projection
	// consume exactly the same effective duration.
	c.Set("task_request", normalized)
	return nil
}

// EstimateBilling 根据用户请求的 seconds 和 size 计算 OtherRatios。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	// remix 路径的 OtherRatios 已在 ResolveOriginTask 中设置
	if info.Action == constant.TaskActionRemix {
		return nil
	}

	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}

	model := requestDurationModel(c, req, info)
	min, max, supported := durationBoundsForModel(model)
	seconds := 0
	if supported {
		req, err = mergeVideoStudioMetadata(c, req)
		if err != nil {
			return nil
		}
		_, seconds, err = relaycommon.NormalizeTaskDuration(req, min, max)
		if err != nil {
			return nil
		}
	} else {
		// Keep the historical Sora billing behavior for non-Seedance models.
		// The model-specific 4-15/4-30 contract applies only to the special
		// aliases; ordinary Sora requests still use the shared legacy default.
		seconds, _ = strconv.Atoi(req.Seconds)
		if seconds == 0 {
			seconds = req.Duration
		}
		if seconds <= 0 {
			seconds = 4
		}
	}

	size := req.Size
	if size == "" {
		size = "720x1280"
	}

	ratios := map[string]float64{
		"seconds": float64(seconds),
		"size":    1,
	}
	if size == "1792x1024" || size == "1024x1792" {
		ratios["size"] = 1.666667
	}
	return ratios
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if info.Action == constant.TaskActionRemix {
		if isSeedanceSpecialModel(info.UpstreamModelName) || isSeedanceSpecialModel(info.OriginModelName) {
			return "", fmt.Errorf("Seedance special models do not support remix")
		}
		return fmt.Sprintf("%s/v1/videos/%s/remix", a.baseURL, info.OriginTaskID), nil
	}
	return fmt.Sprintf("%s/v1/videos", a.baseURL), nil
}

// BuildRequestHeader sets required headers.
func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", c.Request.Header.Get("Content-Type"))
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, errors.Wrap(err, "get_request_body_failed")
	}
	cachedBody, err := storage.Bytes()
	if err != nil {
		return nil, errors.Wrap(err, "read_body_bytes_failed")
	}
	contentType := c.GetHeader("Content-Type")

	if strings.HasPrefix(contentType, "application/json") {
		if shouldProjectVideoStudioRequest(c, info) {
			newBody, err := buildVideoStudioRequest(cachedBody, info.UpstreamModelName)
			if err != nil {
				return nil, err
			}
			newBody, err = canonicalizeSoraJSONRequest(newBody, info.UpstreamModelName)
			if err != nil {
				return nil, err
			}
			return bytes.NewReader(newBody), nil
		}
		newBody, err := canonicalizeSoraJSONRequest(cachedBody, info.UpstreamModelName)
		if err != nil {
			return nil, err
		}
		return bytes.NewReader(newBody), nil
	}

	if strings.Contains(contentType, "multipart/form-data") {
		if isSeedanceSpecialModel(requestModelHint(c, info)) {
			return nil, fmt.Errorf("Seedance special models accept application/json requests only")
		}
		formData, err := common.ParseMultipartFormReusable(c)
		if err != nil {
			return bytes.NewReader(cachedBody), nil
		}
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		writer.WriteField("model", info.UpstreamModelName)
		if min, max, supported := durationBoundsForModel(info.UpstreamModelName); supported {
			taskReq, taskErr := relaycommon.GetTaskRequest(c)
			if taskErr != nil {
				return nil, taskErr
			}
			taskReq, taskErr = mergeVideoStudioMetadata(c, taskReq)
			if taskErr != nil {
				return nil, taskErr
			}
			normalized, _, normalizeErr := relaycommon.NormalizeTaskDuration(taskReq, min, max)
			if normalizeErr != nil {
				return nil, normalizeErr
			}
			formData.Value["duration"] = []string{strconv.Itoa(normalized.Duration)}
			if _, present := formData.Value["seconds"]; present {
				formData.Value["seconds"] = []string{strconv.Itoa(normalized.Duration)}
			}
		}
		for key, values := range formData.Value {
			if key == "model" {
				continue
			}
			for _, v := range values {
				writer.WriteField(key, v)
			}
		}
		for fieldName, fileHeaders := range formData.File {
			for _, fh := range fileHeaders {
				f, err := fh.Open()
				if err != nil {
					continue
				}
				ct := fh.Header.Get("Content-Type")
				if ct == "" || ct == "application/octet-stream" {
					buf512 := make([]byte, 512)
					n, _ := io.ReadFull(f, buf512)
					ct = http.DetectContentType(buf512[:n])
					// Re-open after sniffing so the full content is copied below
					f.Close()
					f, err = fh.Open()
					if err != nil {
						continue
					}
				}
				h := make(textproto.MIMEHeader)
				h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, fieldName, fh.Filename))
				h.Set("Content-Type", ct)
				part, err := writer.CreatePart(h)
				if err != nil {
					f.Close()
					continue
				}
				io.Copy(part, f)
				f.Close()
			}
		}
		writer.Close()
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())
		return &buf, nil
	}

	return common.NewReplayableBodyReader(storage), nil
}

func shouldProjectVideoStudioRequest(c *gin.Context, info *relaycommon.RelayInfo) bool {
	if c == nil || c.Request == nil || info == nil || info.ChannelMeta == nil {
		return false
	}
	return c.Request.Method == http.MethodPost && strings.TrimRight(c.Request.URL.Path, "/") == "/pg/videos"
}

func buildVideoStudioRequest(body []byte, upstreamModel string) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(body, &fields); err != nil {
		return nil, errors.Wrap(err, "decode_video_studio_request_failed")
	}
	if fields == nil {
		return nil, fmt.Errorf("decode_video_studio_request_failed: request body must be an object")
	}

	if rawMetadata, exists := fields["metadata"]; exists {
		var configuredFields map[string]json.RawMessage
		if err := common.Unmarshal(rawMetadata, &configuredFields); err != nil {
			return nil, errors.Wrap(err, "decode_video_studio_metadata_failed")
		}
		// Duration fields may be stored in metadata by Video Studio. Promote
		// them before removing metadata so provider projection and billing see
		// the same request values.
		for _, key := range []string{"duration", "seconds"} {
			if _, topLevel := fields[key]; !topLevel {
				if value, configured := configuredFields[key]; configured {
					fields[key] = value
				}
			}
		}
		// Video Studio records configured request keys in metadata, then adds
		// image/images aliases for generic task validation compatibility.
		for _, alias := range []string{"image", "images"} {
			if _, configured := configuredFields[alias]; !configured {
				delete(fields, alias)
			}
		}
	}

	delete(fields, "group")
	delete(fields, "mode")
	delete(fields, "metadata")
	modelJSON, err := common.Marshal(upstreamModel)
	if err != nil {
		return nil, errors.Wrap(err, "encode_video_studio_model_failed")
	}
	fields["model"] = modelJSON
	projected, err := common.Marshal(fields)
	if err != nil {
		return nil, errors.Wrap(err, "encode_video_studio_request_failed")
	}
	return projected, nil
}

// canonicalizeSoraJSONRequest writes the provider-facing model and duration.
// The special endpoints require an integer duration in their model-specific
// range. Keep a supplied seconds field for compatibility, but make it agree
// with duration.
func canonicalizeSoraJSONRequest(body []byte, upstreamModel string) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(body, &fields); err != nil {
		return nil, errors.Wrap(err, "decode_sora_request_failed")
	}
	if fields == nil {
		return nil, fmt.Errorf("decode_sora_request_failed: request body must be an object")
	}
	modelJSON, err := common.Marshal(upstreamModel)
	if err != nil {
		return nil, errors.Wrap(err, "encode_sora_model_failed")
	}
	min, max, supported := durationBoundsForModel(upstreamModel)
	if !supported {
		fields["model"] = modelJSON
		return common.Marshal(fields)
	}
	var req relaycommon.TaskSubmitReq
	if err := common.Unmarshal(body, &req); err != nil {
		return nil, errors.Wrap(err, "decode_sora_duration_failed")
	}
	_, effective, err := relaycommon.NormalizeTaskDuration(req, min, max)
	if err != nil {
		return nil, errors.Wrap(err, "invalid_duration")
	}
	fields["model"] = modelJSON
	fields["duration"] = json.RawMessage(strconv.Itoa(effective))
	if _, supplied := fields["seconds"]; supplied {
		if common.GetJsonType(fields["seconds"]) == "string" {
			fields["seconds"], err = common.Marshal(strconv.Itoa(effective))
		} else {
			fields["seconds"] = json.RawMessage(strconv.Itoa(effective))
		}
		if err != nil {
			return nil, errors.Wrap(err, "encode_sora_seconds_failed")
		}
	}
	return common.Marshal(fields)
}

// DoRequest delegates to common helper.
func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

// DoResponse handles upstream response, returns taskID etc.
func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*channel.TaskSubmitResponse, *channel.TaskResponseError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, channel.NewUncertainTaskResponseError(service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError))
	}
	_ = resp.Body.Close()

	// Parse Sora response
	var dResp responseTask
	if err := common.Unmarshal(responseBody, &dResp); err != nil {
		return nil, channel.NewUncertainTaskResponseError(service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError))
	}

	upstreamID := dResp.ID
	if upstreamID == "" {
		upstreamID = dResp.TaskID
	}
	if upstreamID == "" {
		return nil, channel.NewUncertainTaskResponseError(service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError))
	}

	// 使用公开 task_xxxx ID 返回给客户端
	dResp.ID = info.PublicTaskID
	dResp.TaskID = info.PublicTaskID
	buffered, err := channel.NewJSONTaskSubmitResponse(upstreamID, responseBody, dResp)
	if err != nil {
		return nil, channel.NewUncertainTaskResponseError(service.TaskErrorWrapper(err, "marshal_response_failed", http.StatusInternalServerError))
	}
	return buffered, nil
}

// FetchTask fetch task status
func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := fmt.Sprintf("%s/v1/videos/%s", baseUrl, taskID)

	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	resTask := responseTask{}
	if err := common.Unmarshal(respBody, &resTask); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := relaycommon.TaskInfo{
		Code: 0,
	}

	switch resTask.Status {
	case "queued", "pending":
		taskResult.Status = model.TaskStatusQueued
	case "processing", "in_progress":
		taskResult.Status = model.TaskStatusInProgress
	case "completed":
		taskResult.Status = model.TaskStatusSuccess
		// Url intentionally left empty — the caller constructs the proxy URL using the public task ID
	case "failed", "cancelled":
		taskResult.Status = model.TaskStatusFailure
		if resTask.Error != nil {
			taskResult.Reason = resTask.Error.Message
		} else {
			taskResult.Reason = "task failed"
		}
	default:
	}
	if resTask.Progress > 0 && resTask.Progress < 100 {
		taskResult.Progress = fmt.Sprintf("%d%%", resTask.Progress)
	}

	return &taskResult, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	data := task.Data
	var err error
	if data, err = sjson.SetBytes(data, "id", task.TaskID); err != nil {
		return nil, errors.Wrap(err, "set id failed")
	}
	if data, err = sjson.SetBytes(data, "task_id", task.TaskID); err != nil {
		return nil, errors.Wrap(err, "set task_id failed")
	}
	if data, err = sjson.DeleteBytes(data, "video_url"); err != nil {
		return nil, errors.Wrap(err, "delete video_url failed")
	}
	if task.PublicResultURL() != "" {
		if data, err = sjson.SetBytes(data, "video_url", taskcommon.BuildProxyURL(task.TaskID)); err != nil {
			return nil, errors.Wrap(err, "set video_url failed")
		}
	}
	return data, nil
}
