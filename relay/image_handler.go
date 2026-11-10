package relay

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func ImageHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {
	info.InitChannelMeta(c)
	info.BillingImageCount = nil
	info.ImageRequestCount = 0
	info.ImagePricingOutboundValidated = false
	info.UpstreamIsStream = false

	imageReq, ok := info.Request.(*dto.ImageRequest)
	if !ok {
		return types.NewErrorWithStatusCode(fmt.Errorf("invalid request type, expected dto.ImageRequest, got %T", info.Request), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	request, err := common.DeepCopy(imageReq)
	if err != nil {
		return types.NewError(fmt.Errorf("failed to copy request to ImageRequest: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	info.UpstreamIsStream = info.IsStream

	err = helper.ModelMappedHelper(c, info, request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)
	imageCount, err := request.ImageCount(false)
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	var jsonData []byte
	var requestBody io.Reader
	var requestBodyCloser io.Closer
	defer func() {
		if requestBodyCloser != nil {
			_ = requestBodyCloser.Close()
		}
	}()

	if model_setting.GetGlobalSettings().PassThroughRequestEnabled || info.ChannelSetting.PassThroughBodyEnabled {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		if info.ImagePricingSnapshot == nil {
			if strings.Contains(c.Request.Header.Get("Content-Type"), "multipart/form-data") {
				requestBody = common.NewReplayableBodyReader(storage)
			} else {
				jsonData, err = storage.Bytes()
				if err != nil {
					return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
				}
			}
		} else {
			requestBody, _, requestBodyCloser, err = buildPricedImagePassthroughBody(c, info, storage)
			if err != nil {
				return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
			}
		}
	} else {
		convertedRequest, err := adaptor.ConvertImageRequest(c, info, *request)
		if err != nil {
			var apiErr *types.NewAPIError
			if errors.As(err, &apiErr) {
				return apiErr
			}
			return types.NewError(err, types.ErrorCodeConvertRequestFailed)
		}
		relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)

		switch convertedRequest.(type) {
		case *bytes.Buffer:
			requestBody = convertedRequest.(io.Reader)
		default:
			jsonData, err = common.Marshal(convertedRequest)
			if err != nil {
				return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			}

			// apply param override
			if len(info.ParamOverride) > 0 {
				jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
				if err != nil {
					return newAPIErrorFromParamOverride(err)
				}
			}
		}
	}
	if jsonData != nil {
		if err := relaycommon.ValidateOutboundImagePricingJSON(info, jsonData); err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		// Overrides and passthrough may change the quantity after ingress validation.
		var outbound struct {
			N *uint `json:"n"`
		}
		if err := common.Unmarshal(jsonData, &outbound); err != nil {
			return types.NewErrorWithStatusCode(fmt.Errorf("invalid image billing parameters: %w", err), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		if outbound.N != nil {
			quantity := dto.ImageRequest{N: outbound.N}
			imageCount, err = quantity.ImageCount(false)
			if err != nil {
				return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
			}
		}
		info.UpstreamIsStream = gjson.GetBytes(jsonData, "stream").Bool()
		logger.LogDebug(c, "image request body: %s", jsonData)
		body, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		requestBodyCloser = closer
		requestBody = body
	} else if info.ImageRequestCount > 0 {
		// Multipart adaptors validate the effective fields while rebuilding the body.
		imageCount = info.ImageRequestCount
	}

	if info.ImagePricingSnapshot != nil && !info.ImagePricingOutboundValidated {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("image pricing outbound request was not validated"),
			types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry(),
		)
	}

	if billingErr := service.PrepareImageBillingForRequest(c, info, imageCount); billingErr != nil {
		return billingErr
	}

	statusCodeMappingStr := c.GetString("status_code_mapping")

	if err := service.MarkImageStudioGenerationDispatching(c); err != nil {
		return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		newAPIError := types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
		var existing *types.NewAPIError
		if !errors.As(err, &existing) {
			newAPIError.Err = err
		}
		return newAPIError
	}
	var httpResp *http.Response
	if resp != nil {
		httpResp = resp.(*http.Response)
		info.UpstreamIsStream = strings.HasPrefix(strings.ToLower(httpResp.Header.Get("Content-Type")), "text/event-stream")
		if httpResp.StatusCode != http.StatusOK {
			if httpResp.StatusCode == http.StatusCreated && info.ApiType == constant.APITypeReplicate {
				// replicate channel returns 201 Created when using Prefer: wait, treat it as success.
				httpResp.StatusCode = http.StatusOK
			} else {
				newAPIError = service.RelayErrorHandler(c.Request.Context(), httpResp, false)
				// reset status code 重置状态码
				service.ResetStatusCode(newAPIError, statusCodeMappingStr)
				return newAPIError
			}
		}
	}

	usage, newAPIError := adaptor.DoResponse(c, httpResp, info)
	service.CaptureFailedRequestUsage(c, info, usage, newAPIError)
	if newAPIError != nil {
		if httpResp != nil {
			types.ErrOptionWithOriginalStatusCode(httpResp.StatusCode)(newAPIError)
		}
		// reset status code 重置状态码
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}

	imageN := info.RequestedImageCount()
	if info.BillingImageCount != nil {
		imageN = *info.BillingImageCount
	} else if count, ok := info.PriceData.OtherRatios()["n"]; ok && info.PriceData.UsePrice && count >= 1 && count <= dto.MaxImageN {
		imageN = common.QuotaRound(count)
	}

	if usage.(*dto.Usage).TotalTokens == 0 {
		usage.(*dto.Usage).TotalTokens = 1
	}
	if usage.(*dto.Usage).PromptTokens == 0 {
		usage.(*dto.Usage).PromptTokens = 1
	}

	quality := request.Quality
	if quality == "" {
		quality = "standard"
	}

	var logContent []*common.Message

	if len(request.Size) > 0 {
		logContent = append(logContent, common.NewMessage("Size {{size}}", map[string]any{"size": request.Size}))
	}
	if len(quality) > 0 {
		logContent = append(logContent, common.NewMessage("Quality {{quality}}", map[string]any{"quality": quality}))
	}
	if imageN > 0 {
		logContent = append(logContent, common.NewMessage("Image count {{count}}", map[string]any{"count": imageN}))
	}

	service.PostTextConsumeQuota(c, info, usage.(*dto.Usage), logContent)
	return nil
}
