package openai

import (
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type membershipRealtimeFrame struct {
	message []byte
	err     error
}

type membershipRealtimeTurn struct {
	info       *relaycommon.RelayInfo
	context    *gin.Context
	responseID string
	deadline   time.Time
}

func readMembershipRealtime(conn *websocket.Conn, frames chan<- membershipRealtimeFrame, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	for {
		_, message, err := conn.ReadMessage()
		select {
		case frames <- membershipRealtimeFrame{message: message, err: err}:
		case <-stop:
			return
		}
		if err != nil {
			return
		}
	}
}

func membershipRealtimeError(message string) *types.NewAPIError {
	return types.NewErrorWithStatusCode(fmt.Errorf("%s", message), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

// All protocol state, websocket writes and billing mutations belong to this one
// loop. Reader goroutines only deliver frames, including during policy draining.
func membershipRealtimeHandler(c *gin.Context, info *relaycommon.RelayInfo) (resultErr *types.NewAPIError, resultUsage *dto.RealtimeUsage) {
	info.IsStream = true
	maxMB := constant.MaxRequestBodyMB
	if maxMB <= 0 || int64(maxMB) > math.MaxInt64>>20 {
		maxMB = 128
	}
	info.ClientWs.SetReadLimit(int64(maxMB) << 20)
	info.TargetWs.SetReadLimit(int64(maxMB) << 20)
	clientFrames, upstreamFrames := make(chan membershipRealtimeFrame), make(chan membershipRealtimeFrame)
	clientDone, upstreamDone, stop := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go readMembershipRealtime(info.ClientWs, clientFrames, stop, clientDone)
	go readMembershipRealtime(info.TargetWs, upstreamFrames, stop, upstreamDone)
	defer func() {
		close(stop)
		// The controller still needs the client write side to send its sanitized
		// error frame. A read deadline stops this reader without closing that side.
		_ = info.ClientWs.SetReadDeadline(time.Now())
		_ = info.TargetWs.Close()
		<-clientDone
		<-upstreamDone
		_ = info.ClientWs.SetWriteDeadline(time.Now().Add(10 * time.Second))
	}()
	var active *membershipRealtimeTurn
	var deadline, drain <-chan time.Time
	var responseTimer, drainTimer *time.Timer
	defer func() {
		if responseTimer != nil {
			responseTimer.Stop()
		}
		if drainTimer != nil {
			drainTimer.Stop()
		}
		if active != nil {
			if err := model.MarkMembershipBillingUncertain(active.info.MembershipBillingRequestID, active.info.ChannelId, "realtime_response_outcome_unknown"); err != nil {
				logger.LogError(c, "failed to flag realtime authorization: "+err.Error())
			}
		}
	}()
	upstreamEnded := false
	defer func() {
		// A client protocol error may race an already-dispatched response. Drain
		// that response under its original deadline, preserving upstream policy.
		if active == nil || upstreamEnded || !time.Now().Before(active.deadline) {
			return
		}
		timer := time.NewTimer(time.Until(active.deadline))
		defer timer.Stop()
		for {
			select {
			case <-timer.C:
				return
			case frame := <-upstreamFrames:
				if frame.err != nil {
					return
				}
				var event dto.RealtimeEvent
				if common.Unmarshal(frame.message, &event) != nil {
					return
				}
				if policyErr := realtimePolicyError(&event); policyErr != nil {
					resultErr = policyErr
					return
				}
				if active == nil {
					continue
				}
				var fields struct {
					Response struct {
						ID     string `json:"id"`
						Status string `json:"status"`
					} `json:"response"`
				}
				if common.Unmarshal(frame.message, &fields) != nil {
					return
				}
				if event.Type == "response.created" && active.responseID == "" {
					active.responseID = fields.Response.ID
				}
				if event.Type == "response.done" {
					if settleMembershipRealtimeTurn(active, fields.Response.ID, fields.Response.Status, event.Response) != nil {
						return
					}
					active = nil
				}
			}
		}
	}()
	requestDone := c.Done()
	clientGone, sessionReady, sessionPending := false, false, false
	connectionID, turnNumber := common.NewRequestId(), 0
	var pendingInput, priorUsage int64
	for {
		select {
		case <-requestDone:
			clientGone, requestDone = true, nil
			clientFrames = nil
			if drainTimer == nil {
				drainTimer = time.NewTimer(realtimePolicyDrainDuration())
				drain = drainTimer.C
			}
		case <-drain:
			if active != nil {
				return membershipRealtimeError("realtime disconnected before final usage; authorization retained for review"), nil
			}
			return nil, nil
		case <-deadline:
			upstreamEnded = true
			return membershipRealtimeError("realtime response exceeded its authorized time limit; authorization retained for review"), nil
		case frame := <-clientFrames:
			if frame.err != nil {
				clientGone, clientFrames, requestDone = true, nil, nil
				if drainTimer == nil {
					drainTimer = time.NewTimer(realtimePolicyDrainDuration())
					drain = drainTimer.C
				}
				continue
			}
			var event map[string]any
			if err := common.Unmarshal(frame.message, &event); err != nil || event == nil {
				return membershipRealtimeError("invalid realtime event"), nil
			}
			kind, _ := event["type"].(string)
			frameTokens := int64((len(frame.message) + 3) / 4)
			if kind == "input_audio_buffer.append" {
				frameTokens = int64((len(frame.message) + 511) / 512)
			}
			if pendingInput+priorUsage+frameTokens > dto.MaxTokensLimit {
				return membershipRealtimeError("realtime input estimate exceeds the supported billing limit"), nil
			}
			switch kind {
			case "session.update":
				if active != nil || sessionPending {
					return membershipRealtimeError("wait for the current response/session update before changing the session"), nil
				}
				session, _ := event["session"].(map[string]any)
				if err := validateMembershipRealtimeSession(session, info.UpstreamModelName); err != nil {
					return membershipRealtimeError(err.Error()), nil
				}
				sessionReady, sessionPending = false, true
			case "response.create":
				if !sessionReady || active != nil {
					return membershipRealtimeError("confirm a manual realtime session and finish the previous response before response.create"), nil
				}
				response, _ := event["response"].(map[string]any)
				if event["response"] != nil && response == nil {
					return membershipRealtimeError("response must be an object"), nil
				}
				if response == nil {
					response = make(map[string]any)
					event["response"] = response
				}
				if err := validateMembershipRealtimeResponse(response); err != nil {
					return membershipRealtimeError(err.Error()), nil
				}
				turnNumber++
				turnInfo := *info
				turnInfo.RequestId = fmt.Sprintf("rt:%s:%d", connectionID, turnNumber)
				turnInfo.StartTime = time.Now()
				turnInfo.ResetAttemptTiming()
				turnInfo.StreamStatus = relaycommon.NewStreamStatus()
				turnInfo.StreamStatus.RequireTerminal()
				turnInfo.Billing, turnInfo.MembershipPricing, turnInfo.TieredBillingSnapshot = nil, nil, nil
				turnContext := c.Copy()
				turnContext.Set(common.RequestIdKey, turnInfo.RequestId)
				price, err := helper.ModelPriceHelper(turnContext, &turnInfo, 0, &types.TokenCountMeta{})
				if err != nil {
					return membershipRealtimeError(err.Error()), nil
				}
				turnInfo.PriceData = price
				outputLimit, err := membershipRealtimeOutputLimit(response["max_output_tokens"])
				if err != nil {
					return membershipRealtimeError(err.Error()), nil
				}
				if err := service.PrepareRealtimeMembershipBudget(&turnInfo, int(pendingInput+priorUsage+frameTokens), outputLimit); err != nil {
					return membershipRealtimeError(err.Error()), nil
				}
				if apiErr := service.PreConsumeBilling(turnContext, 0, &turnInfo); apiErr != nil {
					return apiErr, nil
				}
				responseDeadline := time.Now().Add(realtimePolicyDrainDuration())
				active = &membershipRealtimeTurn{info: &turnInfo, context: turnContext, deadline: responseDeadline}
				if err := model.MarkMembershipBillingDispatched(turnInfo.MembershipBillingRequestID, turnInfo.ChannelId, responseDeadline.Unix()); err != nil {
					turnInfo.Billing.Refund(turnContext)
					active = nil
					return membershipRealtimeError("cannot record realtime dispatch authorization"), nil
				}
				responseTimer = time.NewTimer(time.Until(responseDeadline))
				deadline = responseTimer.C
			case "response.cancel":
				if active == nil {
					return membershipRealtimeError("no authorized realtime response is active"), nil
				}
			case "input_audio_buffer.append", "input_audio_buffer.commit", "input_audio_buffer.clear", "conversation.item.create", "conversation.item.delete", "conversation.item.truncate", "conversation.item.retrieve":
				if !sessionReady || active != nil {
					return membershipRealtimeError("finish session confirmation and the current response before changing realtime input"), nil
				}
				if containsRealtimeImage(event) {
					return membershipRealtimeError("image input is not supported by realtime membership pricing"), nil
				}
			default:
				return membershipRealtimeError("unsupported event for manual realtime membership billing: " + kind), nil
			}
			pendingInput += frameTokens
			encoded, err := common.Marshal(event)
			if err != nil || writeMembershipRealtime(info.TargetWs, encoded, active) != nil {
				return membershipRealtimeError("failed to send the authorized realtime event"), nil
			}
		case frame := <-upstreamFrames:
			if frame.err != nil {
				upstreamEnded = true
				if active != nil {
					return membershipRealtimeError("upstream closed before final realtime usage; authorization retained for review"), nil
				}
				return nil, nil
			}
			var event dto.RealtimeEvent
			if err := common.Unmarshal(frame.message, &event); err != nil {
				return membershipRealtimeError("invalid upstream realtime event"), nil
			}
			if policyErr := realtimePolicyError(&event); policyErr != nil {
				upstreamEnded = true
				return policyErr, nil
			}
			var fields struct {
				Session  map[string]any `json:"session"`
				Response struct {
					ID     string `json:"id"`
					Status string `json:"status"`
				} `json:"response"`
			}
			if err := common.Unmarshal(frame.message, &fields); err != nil {
				return membershipRealtimeError("invalid upstream realtime fields"), nil
			}
			switch event.Type {
			case "session.updated":
				if !sessionPending {
					return membershipRealtimeError("unexpected upstream session change"), nil
				}
				if err := validateMembershipRealtimeSession(fields.Session, info.UpstreamModelName); err != nil {
					return membershipRealtimeError("upstream did not confirm safe manual realtime settings: " + err.Error()), nil
				}
				sessionPending, sessionReady = false, true
			case "response.created":
				if active == nil || active.responseID != "" || fields.Response.ID == "" {
					return membershipRealtimeError("upstream started an unauthorized or concurrent realtime response"), nil
				}
				active.responseID = fields.Response.ID
				active.info.SetFirstResponseTime()
			case "response.done":
				if err := settleMembershipRealtimeTurn(active, fields.Response.ID, fields.Response.Status, event.Response); err != nil {
					upstreamEnded = true
					return membershipRealtimeError(err.Error()), nil
				}
				priorUsage, pendingInput = int64(event.Response.Usage.TotalTokens), 0
				active, deadline = nil, nil
				responseTimer.Stop()
			case "error":
				if active != nil {
					upstreamEnded = true
					return membershipRealtimeError("upstream response failed without final usage; authorization retained for review"), nil
				}
			}
			if !clientGone {
				if err := writeMembershipRealtime(info.ClientWs, frame.message, active); err != nil {
					clientGone, clientFrames, requestDone = true, nil, nil
					if drainTimer == nil {
						drainTimer = time.NewTimer(realtimePolicyDrainDuration())
						drain = drainTimer.C
					}
				}
			}
		}
	}
}

func writeMembershipRealtime(conn *websocket.Conn, message []byte, turn *membershipRealtimeTurn) error {
	deadline := time.Now().Add(10 * time.Second)
	if turn != nil && turn.deadline.Before(deadline) {
		deadline = turn.deadline
	}
	if err := conn.SetWriteDeadline(deadline); err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, message)
}

func settleMembershipRealtimeTurn(turn *membershipRealtimeTurn, responseID, status string, response *dto.RealtimeResponse) error {
	if turn == nil || turn.responseID == "" || turn.responseID != responseID || response == nil || !validMembershipRealtimeUsage(response.Usage) {
		return fmt.Errorf("missing or inconsistent final realtime usage; authorization retained for review")
	}
	turn.info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
	switch status {
	case "failed":
		turn.info.StreamStatus.MarkFailed("", "", 0)
	case "cancelled":
		turn.info.StreamStatus.MarkCancelled()
	case "incomplete":
		turn.info.StreamStatus.MarkIncomplete("")
	default:
		turn.info.StreamStatus.MarkCompleted()
	}
	service.PostWssConsumeQuota(turn.context, turn.info, turn.info.GetBillingModelName(), response.Usage, "")
	record, err := model.GetMembershipBillingRecord(turn.info.MembershipBillingRequestID)
	if err != nil || record.State != "settled" {
		return fmt.Errorf("realtime settlement pending; authorization retained for review")
	}
	return nil
}

func validateMembershipRealtimeSession(session map[string]any, modelName string) error {
	if session == nil {
		return fmt.Errorf("session must explicitly disable automatic responses with turn_detection=null or create_response=false")
	}
	if name, exists := session["model"]; exists && name != modelName {
		return fmt.Errorf("realtime model cannot change after connection")
	}
	if kind, exists := session["type"]; exists && kind != "realtime" {
		return fmt.Errorf("only realtime generation sessions support per-response membership authorization")
	}
	if err := validateMembershipRealtimeTools(session["tools"]); err != nil {
		return err
	}
	turn, explicit := session["turn_detection"]
	if explicit && !manualMembershipRealtimeDetection(turn) {
		return fmt.Errorf("automatic realtime responses must be disabled before billing can be authorized")
	}
	if session["input_audio_transcription"] != nil {
		return fmt.Errorf("independent audio transcription requires separate authorization")
	}
	if audio, ok := session["audio"].(map[string]any); ok {
		if input, ok := audio["input"].(map[string]any); ok {
			if input["transcription"] != nil {
				return fmt.Errorf("independent audio transcription requires separate authorization")
			}
			if value, exists := input["turn_detection"]; exists {
				if !manualMembershipRealtimeDetection(value) {
					return fmt.Errorf("automatic realtime responses must be disabled before billing can be authorized")
				}
				turn, explicit = value, true
			}
		}
	}
	if !explicit {
		return fmt.Errorf("set turn_detection=null or create_response=false and wait for session.updated")
	}
	return nil
}

func manualMembershipRealtimeDetection(value any) bool {
	if value == nil {
		return true
	}
	detection, ok := value.(map[string]any)
	return ok && detection["create_response"] == false
}

func validateMembershipRealtimeTools(value any) error {
	if value == nil {
		return nil
	}
	tools, ok := value.([]any)
	if !ok {
		return fmt.Errorf("invalid realtime tools")
	}
	for _, item := range tools {
		tool, ok := item.(map[string]any)
		if !ok || tool["type"] != "function" {
			return fmt.Errorf("only client-executed function tools are supported for realtime membership billing")
		}
	}
	return nil
}

func validateMembershipRealtimeResponse(response map[string]any) error {
	for key := range response {
		switch key {
		case "modalities", "output_modalities", "instructions", "voice", "output_audio_format", "tools", "tool_choice", "temperature", "max_output_tokens", "conversation", "input", "metadata", "audio":
		default:
			return fmt.Errorf("unsupported realtime response setting: %s", key)
		}
	}
	if containsRealtimeImage(response) {
		return fmt.Errorf("image input is not supported by realtime membership pricing")
	}
	if value := response["audio"]; value != nil {
		audio, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid realtime response audio settings")
		}
		for key := range audio {
			if key != "output" {
				return fmt.Errorf("only output audio can be configured for an authorized response")
			}
		}
	}
	return validateMembershipRealtimeTools(response["tools"])
}

func containsRealtimeImage(value any) bool {
	switch item := value.(type) {
	case map[string]any:
		if item["type"] == "input_image" || item["type"] == "image_url" {
			return true
		}
		for _, child := range item {
			if containsRealtimeImage(child) {
				return true
			}
		}
	case []any:
		for _, child := range item {
			if containsRealtimeImage(child) {
				return true
			}
		}
	}
	return false
}

func membershipRealtimeOutputLimit(value any) (int, error) {
	if value == nil {
		return 1024, nil
	}
	number, ok := value.(float64)
	if !ok || number <= 0 || number > dto.MaxTokensLimit || math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number {
		return 0, fmt.Errorf("max_output_tokens must be a finite positive integer within the billing limit")
	}
	return int(number), nil
}

func validMembershipRealtimeUsage(usage *dto.RealtimeUsage) bool {
	if usage == nil {
		return false
	}
	for _, value := range []int{usage.TotalTokens, usage.InputTokens, usage.OutputTokens, usage.InputTokenDetails.TextTokens, usage.InputTokenDetails.AudioTokens, usage.InputTokenDetails.CachedTokens, usage.OutputTokenDetails.TextTokens, usage.OutputTokenDetails.AudioTokens} {
		if value < 0 || value > math.MaxInt32 {
			return false
		}
	}
	return int64(usage.InputTokens)+int64(usage.OutputTokens) == int64(usage.TotalTokens) &&
		int64(usage.InputTokenDetails.TextTokens)+int64(usage.InputTokenDetails.AudioTokens) == int64(usage.InputTokens) &&
		int64(usage.OutputTokenDetails.TextTokens)+int64(usage.OutputTokenDetails.AudioTokens) == int64(usage.OutputTokens) &&
		usage.InputTokenDetails.CachedTokens <= usage.InputTokens
}
