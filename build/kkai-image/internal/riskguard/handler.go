package riskguard

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

type Handler struct {
	proxy          *httputil.ReverseProxy
	publisher      Publisher
	rules          *RuleSet
	logger         *slog.Logger
	maxBodyBytes   int64
	publishTimeout time.Duration
}

func NewHandler(config Config, publisher Publisher, rules *RuleSet, logger *slog.Logger) *Handler {
	proxy := newReverseProxy(config.Upstream, logger)
	return &Handler{
		proxy:          proxy,
		publisher:      publisher,
		rules:          rules,
		logger:         logger,
		maxBodyBytes:   config.MaxBodyBytes,
		publishTimeout: config.PublishTimeout,
	}
}

func (h *Handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.URL.Path == "/healthz" {
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("ok\n"))
		return
	}
	inspection, err := InspectRequest(request, h.maxBodyBytes)
	if err != nil {
		h.writeUnavailable(response)
		return
	}
	ruleID, matched := h.rules.Match(inspection.Text)
	if !matched {
		h.proxy.ServeHTTP(response, request)
		return
	}

	ctx, cancel := context.WithTimeout(request.Context(), h.publishTimeout)
	defer cancel()
	eventID, err := h.publisher.Publish(ctx, Detection{
		RequestID:        boundedHeader(request.Header.Get("X-Request-Id"), 64),
		Model:            inspection.Model,
		RuleID:           ruleID,
		RuleVersion:      h.rules.Version(),
		EvidenceSHA256:   inspection.BodySHA256,
		TokenFingerprint: TokenFingerprint(request.Header.Get("Authorization")),
		BodyBytes:        inspection.BodyBytes,
	})
	if err != nil {
		h.logger.Error("risk event publication failed", "rule_id", ruleID)
		h.writeUnavailable(response)
		return
	}
	h.logger.Warn("request blocked by edge policy",
		"event_id", eventID,
		"rule_id", ruleID,
		"request_id", boundedHeader(request.Header.Get("X-Request-Id"), 64),
		"body_bytes", inspection.BodyBytes,
	)
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("X-Risk-Case-Id", eventID)
	response.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(response).Encode(map[string]any{
		"error": map[string]string{
			"message": "request blocked by policy",
			"type":    "policy_error",
			"code":    "policy_blocked",
		},
	})
}

func (h *Handler) writeUnavailable(response http.ResponseWriter) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(response).Encode(map[string]any{
		"error": map[string]string{
			"message": "service unavailable",
			"type":    "server_error",
			"code":    "risk_guard_unavailable",
		},
	})
}

func newReverseProxy(target *url.URL, logger *slog.Logger) *httputil.ReverseProxy {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(target)
			request.SetXForwarded()
		},
		FlushInterval: -1,
		ErrorHandler: func(response http.ResponseWriter, _ *http.Request, err error) {
			logger.Error("newapi upstream proxy failed", "error", err)
			http.Error(response, "upstream unavailable", http.StatusBadGateway)
		},
	}
	return proxy
}

func boundedHeader(value string, maxLength int) string {
	value = strings.TrimSpace(value)
	if len(value) > maxLength {
		return value[:maxLength]
	}
	return value
}
