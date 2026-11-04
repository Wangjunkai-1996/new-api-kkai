package openai

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const recoveryControlType = "kkai.recovery.v1"

type recoveryControlFrame struct {
	Type          string          `json:"type"`
	Kind          string          `json:"kind"`
	LogicalID     string          `json:"logical_id"`
	Seq           int             `json:"seq"`
	Attempt       int             `json:"attempt"`
	Outcome       string          `json:"outcome"`
	UsageFormat   string          `json:"usage_format"`
	UsageReported *bool           `json:"usage_reported"`
	Usage         json.RawMessage `json:"usage"`
}

type recoveryV1State struct {
	enabled         bool
	logicalID       string
	seq             int
	lastAttempt     int
	lastKind        string
	activeAttempt   int
	abandoned       bool
	synthetic       bool
	syntheticUsed   bool
	terminal        bool
	outcome         string
	usageReported   bool
	usage           *dto.Usage
	usageFormat     string
	publicEnd       bool
	publicDone      bool
	expectedChoices int
	finishedChoices map[int]bool
	ctx             *gin.Context
	startedAt       time.Time
	firstTextAt     time.Time
	err             error
}

func newRecoveryV1State(c *gin.Context, info *relaycommon.RelayInfo) *recoveryV1State {
	return &recoveryV1State{enabled: c != nil && c.GetBool("kkai_managed_recovery_v1"), expectedChoices: 1, ctx: c, startedAt: info.StartTime}
}

func (s *recoveryV1State) filter(data string) (bool, error) {
	if gjson.Get(data, "type").String() != recoveryControlType {
		if s.enabled {
			if err := s.observePublic(data); err != nil {
				return true, s.fail(err)
			}
			if s.meterAttempt() != 0 && s.firstTextAt.IsZero() {
				eventType := gjson.Get(data, "type").String()
				hasText := (eventType == "response.output_text.delta" || eventType == "response.refusal.delta") && gjson.Get(data, "delta").String() != ""
				for _, choice := range gjson.Get(data, "choices").Array() {
					hasText = hasText || choice.Get("delta.content").String() != "" || choice.Get("delta.refusal").String() != ""
				}
				if hasText {
					s.firstTextAt = time.Now()
					s.recordDiagnostics()
				}
			}
		}
		return false, nil
	}
	if !s.enabled {
		return true, s.fail(errors.New("unnegotiated recovery control frame"))
	}
	var frame recoveryControlFrame
	if err := common.UnmarshalJsonStr(data, &frame); err != nil {
		return true, s.fail(fmt.Errorf("invalid recovery control: %w", err))
	}
	if err := s.consume(frame); err != nil {
		return true, s.fail(err)
	}
	s.recordDiagnostics()
	return true, nil
}

func (s *recoveryV1State) fail(err error) error {
	s.err = err
	s.recordDiagnostics()
	return err
}

func (s *recoveryV1State) recordDiagnostics() {
	if s.ctx == nil || !s.enabled {
		return
	}
	diagnostics := map[string]any{
		"logical_id": s.logicalID, "attempt": s.lastAttempt, "control_seq": s.seq,
		"final_outcome": s.outcome, "usage_reported": s.usageReported,
		"protocol_error": s.err != nil,
	}
	if !s.firstTextAt.IsZero() && !s.startedAt.IsZero() {
		diagnostics["first_real_text_delta_ms"] = max(int64(0), s.firstTextAt.Sub(s.startedAt).Milliseconds())
	}
	s.ctx.Set("kkai_recovery_v1_diagnostics", diagnostics)
}

func (s *recoveryV1State) consume(frame recoveryControlFrame) error {
	if frame.Type != recoveryControlType || frame.LogicalID == "" || len(frame.LogicalID) > 128 ||
		frame.Seq != s.seq+1 || frame.Seq > 45 || frame.Attempt < 0 || frame.Attempt > 11 ||
		(s.logicalID != "" && frame.LogicalID != s.logicalID) || s.terminal {
		return errors.New("invalid recovery control sequence")
	}
	if s.logicalID == "" {
		s.logicalID = frame.LogicalID
	}
	switch frame.Kind {
	case "attempt_begin":
		if s.synthetic || frame.Attempt != s.lastAttempt+1 || (s.lastAttempt > 0 && !s.abandoned) {
			return errors.New("invalid recovery attempt_begin")
		}
		s.activeAttempt = frame.Attempt
		s.lastAttempt = frame.Attempt
		s.abandoned = false
		s.syntheticUsed = false
	case "attempt_abandoned":
		if s.activeAttempt == 0 || frame.Attempt != s.activeAttempt || s.synthetic {
			return errors.New("invalid recovery attempt_abandoned")
		}
		s.activeAttempt = 0
		s.abandoned = true
	case "synthetic_begin":
		if !s.abandoned || s.synthetic || s.syntheticUsed || frame.Attempt != s.lastAttempt {
			return errors.New("invalid recovery synthetic_begin")
		}
		s.synthetic = true
		s.syntheticUsed = true
	case "synthetic_end":
		if !s.synthetic || frame.Attempt != s.lastAttempt {
			return errors.New("invalid recovery synthetic_end")
		}
		s.synthetic = false
	case "terminal":
		abandonedFailure := s.abandoned && frame.Attempt == s.lastAttempt && (frame.Outcome == "failed" || frame.Outcome == "cancelled")
		if s.synthetic || (frame.Attempt != s.activeAttempt && !(frame.Attempt == 0 && s.lastAttempt == 0) && !abandonedFailure) {
			return errors.New("invalid recovery terminal attempt")
		}
		if frame.Outcome != "completed" && frame.Outcome != "incomplete" && frame.Outcome != "failed" && frame.Outcome != "cancelled" {
			return errors.New("invalid recovery terminal outcome")
		}
		if frame.Attempt == 0 && frame.Outcome != "failed" {
			return errors.New("recovery terminal without dispatch must fail")
		}
		usage, reported, err := recoveryTerminalUsage(frame)
		if err != nil {
			return err
		}
		if abandonedFailure && reported {
			return errors.New("abandoned recovery attempt cannot report terminal usage")
		}
		s.usage = usage
		s.usageFormat = frame.UsageFormat
		s.usageReported = reported
		s.outcome = frame.Outcome
		s.terminal = true
	default:
		return errors.New("unknown recovery control kind")
	}
	s.seq = frame.Seq
	s.lastKind = frame.Kind
	return nil
}

func recoveryTerminalUsage(frame recoveryControlFrame) (*dto.Usage, bool, error) {
	if frame.UsageReported == nil || (frame.UsageFormat != "responses" && frame.UsageFormat != "chat") {
		return nil, false, errors.New("invalid recovery terminal usage metadata")
	}
	if !*frame.UsageReported {
		if strings.TrimSpace(string(frame.Usage)) != "null" {
			return nil, false, errors.New("unreported recovery usage must be null")
		}
		return nil, false, nil
	}
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(frame.Usage, &fields); err != nil || fields == nil {
		return nil, false, errors.New("invalid recovery terminal usage")
	}
	if !validRecoveryUsageNumbers(gjson.ParseBytes(frame.Usage), 0) {
		return nil, false, errors.New("negative or excessive nested recovery usage")
	}
	required := []string{"total_tokens"}
	if frame.UsageFormat == "responses" {
		required = append(required, "input_tokens", "output_tokens")
	} else {
		required = append(required, "prompt_tokens", "completion_tokens")
	}
	for _, name := range required {
		var count int
		if value, ok := fields[name]; !ok || common.Unmarshal(value, &count) != nil || count < 0 {
			return nil, false, fmt.Errorf("invalid recovery usage field %s", name)
		}
	}
	var usage dto.Usage
	if err := common.Unmarshal(frame.Usage, &usage); err != nil {
		return nil, false, fmt.Errorf("invalid recovery terminal usage: %w", err)
	}
	if frame.UsageFormat == "responses" {
		return relayconvert.NormalizeResponsesUsage(&usage), true, nil
	}
	return &usage, true, nil
}

func validRecoveryUsageNumbers(value gjson.Result, depth int) bool {
	if depth > 8 || (value.Type == gjson.Number && value.Float() < 0) {
		return false
	}
	valid := true
	if value.IsObject() || value.IsArray() {
		value.ForEach(func(_, child gjson.Result) bool {
			valid = validRecoveryUsageNumbers(child, depth+1)
			return valid
		})
	}
	return valid
}

func (s *recoveryV1State) meterAttempt() int {
	if s == nil || !s.enabled || s.synthetic || s.terminal {
		return 0
	}
	return s.activeAttempt
}

func (s *recoveryV1State) checkEnd() error {
	if s != nil && s.enabled && s.err == nil && (!s.terminal || !s.publicEnd ||
		(s.usageFormat == "chat" && (s.outcome == "completed" || s.outcome == "incomplete") && !s.publicDone)) {
		return s.fail(errors.New("managed recovery stream ended without matching public terminal"))
	}
	if s == nil {
		return nil
	}
	return s.err
}

func (s *recoveryV1State) observePublic(data string) error {
	if s.seq == 0 {
		return errors.New("managed recovery stream lacks attempt_begin")
	}
	if data == "[DONE]" {
		if !s.terminal || !s.publicEnd || s.publicDone || s.outcome == "failed" || s.outcome == "cancelled" {
			return errors.New("unexpected managed recovery DONE")
		}
		s.publicDone = true
		return nil
	}
	eventType := gjson.Get(data, "type").String()
	isError := gjson.Get(data, "error").Exists() || eventType == "error" || eventType == "response.error" || eventType == "response.failed"
	responsesEnd := eventType == "response.completed" || eventType == "response.done" || eventType == "response.incomplete" ||
		eventType == "response.cancelled" || eventType == "response.canceled" || isError
	chatFinish := false
	chatDelta := false
	for _, choice := range gjson.Get(data, "choices").Array() {
		chatFinish = chatFinish || choice.Get("finish_reason").String() != ""
		delta := choice.Get("delta")
		chatDelta = chatDelta || delta.Get("content").String() != "" || delta.Get("reasoning_content").String() != "" ||
			delta.Get("reasoning").String() != "" || delta.Get("refusal").String() != "" || len(delta.Get("tool_calls").Array()) > 0
	}
	if s.terminal {
		if s.publicDone || ((s.usageFormat == "responses" || s.outcome == "failed" || s.outcome == "cancelled") && s.publicEnd) {
			return errors.New("business frame after managed recovery terminal")
		}
		if s.outcome == "failed" || s.outcome == "cancelled" {
			if !isError && eventType != "response.cancelled" && eventType != "response.canceled" {
				return errors.New("managed recovery failure lacks public error")
			}
		} else if s.usageFormat == "responses" {
			if (s.outcome == "completed" && eventType != "response.completed" && eventType != "response.done") ||
				(s.outcome == "incomplete" && eventType != "response.incomplete") {
				return errors.New("managed recovery public outcome mismatch")
			}
			status := gjson.Get(data, "response.status").String()
			if status != "" && status != s.outcome {
				return errors.New("managed recovery response status mismatch")
			}
		} else {
			if isError || chatDelta || (!chatFinish && !gjson.Get(data, "usage").IsObject()) {
				return errors.New("invalid managed recovery Chat terminal sequence")
			}
			for _, choice := range gjson.Get(data, "choices").Array() {
				if choice.Get("finish_reason").String() == "" {
					continue
				}
				index := int(choice.Get("index").Int())
				if index < 0 || index >= max(1, s.expectedChoices) || s.finishedChoices[index] {
					return errors.New("invalid managed recovery Chat finish choice")
				}
				if s.finishedChoices == nil {
					s.finishedChoices = make(map[int]bool)
				}
				s.finishedChoices[index] = true
			}
		}
		if responsesEnd || (chatFinish && len(s.finishedChoices) == max(1, s.expectedChoices)) {
			s.publicEnd = true
		}
		return nil
	}
	if responsesEnd || chatFinish || gjson.Get(data, "usage").IsObject() {
		return errors.New("public terminal precedes managed recovery terminal")
	}
	if s.synthetic {
		if eventType == "response.output_text.delta" {
			return nil
		}
		for _, choice := range gjson.Get(data, "choices").Array() {
			delta := choice.Get("delta")
			if delta.Get("reasoning_content").String() != "" || delta.Get("reasoning").String() != "" || delta.Get("refusal").String() != "" || len(delta.Get("tool_calls").Array()) > 0 {
				return errors.New("non-text managed recovery synthetic frame")
			}
		}
		if !chatDelta {
			return errors.New("invalid managed recovery synthetic frame")
		}
		return nil
	}
	if s.activeAttempt == 0 {
		switch eventType {
		case "response.output_item.added":
			item := gjson.Get(data, "item")
			if item.Get("type").String() == "message" && item.Get("role").String() == "assistant" && len(item.Get("content").Array()) == 0 {
				return nil
			}
		case "response.content_part.added":
			part := gjson.Get(data, "part")
			if part.Get("type").String() == "output_text" && part.Get("text").String() == "" {
				return nil
			}
		case "response.output_text.done", "response.refusal.done", "response.content_part.done", "response.reasoning_summary_text.done", "response.reasoning_summary_part.done":
			if !gjson.Get(data, "encrypted_content").Exists() && !gjson.Get(data, "part.encrypted_content").Exists() {
				return nil // Close the already-delivered text or summary after abandoning its meter.
			}
		case "response.output_item.done":
			item := gjson.Get(data, "item")
			if (item.Get("type").String() == "message" || item.Get("type").String() == "reasoning") && !item.Get("encrypted_content").Exists() {
				return nil
			}
		}
		return errors.New("business frame outside managed recovery attempt")
	}
	return nil
}

func (s *recoveryV1State) finalUsage(info *relaycommon.RelayInfo, fallback *dto.Usage) *dto.Usage {
	if s == nil || !s.enabled {
		return fallback
	}
	info.BillingHasReportedUsage = s.usageReported
	if s.usageReported {
		info.BillingOutcomeUnknown = false
	} else if (s.outcome == "completed" || s.outcome == "incomplete") && info.HasDurableBilling() && !info.PriceData.UsePrice && info.ImagePricingSnapshot == nil {
		info.BillingOutcomeUnknown = true
	}
	if s.usageReported {
		return s.usage
	}
	return fallback
}
