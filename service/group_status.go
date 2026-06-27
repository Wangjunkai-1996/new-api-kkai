package service

import (
	"math"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/setting"
)

const (
	GroupHealthOperational = "operational"
	GroupHealthBusy        = "busy"
	GroupHealthDegraded    = "degraded"
	GroupHealthOutage      = "outage"
	GroupHealthUnknown     = "unknown"

	GroupHealthConfidenceHigh   = "high"
	GroupHealthConfidenceMedium = "medium"
	GroupHealthConfidenceLow    = "low"

	groupHealthMinSamples        = int64(20)
	groupHealthMediumSamples     = int64(100)
	groupHealthOutageSuccessRate = 80.0
	groupHealthDegradedRate      = 95.0
	groupHealthBusyLatencyMs     = int64(15000)
	groupHealthBusyTtftMs        = int64(5000)
)

type GroupStatusRequest struct {
	UsableGroups map[string]string
	Hours        int
}

type GroupStatusResult struct {
	GeneratedAt int64              `json:"generated_at"`
	WindowHours int                `json:"window_hours"`
	Groups      []GroupStatusEntry `json:"groups"`
}

type GroupStatusEntry struct {
	Group               string  `json:"group"`
	Desc                string  `json:"desc"`
	Status              string  `json:"status"`
	Confidence          string  `json:"confidence"`
	Message             string  `json:"message"`
	RequestCount        int64   `json:"request_count"`
	SuccessRate         float64 `json:"success_rate"`
	AvgLatencyMs        int64   `json:"avg_latency_ms"`
	AvgTtftMs           int64   `json:"avg_ttft_ms"`
	AvailableModelCount int64   `json:"available_model_count"`
	UpdatedAt           int64   `json:"updated_at"`
}

type groupMetrics struct {
	requestCount   int64
	successCount   int64
	totalLatencyMs int64
	ttftSumMs      int64
	ttftCount      int64
}

func GetUserGroupStatuses(req GroupStatusRequest) (GroupStatusResult, error) {
	hours := normalizeGroupStatusHours(req.Hours)
	groupNames := sortedUsableGroupNames(req.UsableGroups)
	if len(groupNames) == 0 {
		return GroupStatusResult{
			GeneratedAt: time.Now().Unix(),
			WindowHours: hours,
			Groups:      []GroupStatusEntry{},
		}, nil
	}

	metrics, err := loadGroupMetrics(hours, groupNames)
	if err != nil {
		return GroupStatusResult{}, err
	}
	applyAutoGroupMetrics(metrics, req.UsableGroups)
	modelCounts, err := loadGroupModelCounts(groupNames)
	if err != nil {
		return GroupStatusResult{}, err
	}
	if err := applyAutoGroupModelCount(modelCounts, req.UsableGroups); err != nil {
		return GroupStatusResult{}, err
	}

	entries := make([]GroupStatusEntry, 0, len(groupNames))
	now := time.Now().Unix()
	for _, group := range groupNames {
		entry := buildGroupStatusEntry(group, req.UsableGroups[group], metrics[group], modelCounts[group], now)
		entries = append(entries, entry)
	}

	return GroupStatusResult{
		GeneratedAt: now,
		WindowHours: hours,
		Groups:      entries,
	}, nil
}

func normalizeGroupStatusHours(hours int) int {
	switch hours {
	case 1, 6, 24:
		return hours
	default:
		return 24
	}
}

func sortedUsableGroupNames(groups map[string]string) []string {
	names := make([]string, 0, len(groups))
	for group := range groups {
		names = append(names, group)
	}
	sort.Strings(names)
	return names
}

func loadGroupMetrics(hours int, groups []string) (map[string]groupMetrics, error) {
	endTs := time.Now().Unix()
	startTs := endTs - int64(hours)*3600
	rows, err := model.GetPerfMetricGroupSummaries(startTs, endTs, groups)
	if err != nil {
		return nil, err
	}

	metrics := make(map[string]groupMetrics, len(rows))
	for _, row := range rows {
		current := metrics[row.Group]
		current.requestCount += row.RequestCount
		current.successCount += row.SuccessCount
		current.totalLatencyMs += row.TotalLatencyMs
		current.ttftSumMs += row.TtftSumMs
		current.ttftCount += row.TtftCount
		metrics[row.Group] = current
	}

	hotResult, err := perfmetrics.QuerySummaryByGroup(hours, groups)
	if err != nil {
		return nil, err
	}
	for _, item := range hotResult.Groups {
		current := metrics[item.Group]
		current.requestCount += item.RequestCount
		current.successCount += item.SuccessCount
		current.totalLatencyMs += item.TotalLatencyMs
		current.ttftSumMs += item.TtftSumMs
		current.ttftCount += item.TtftCount
		metrics[item.Group] = current
	}

	return metrics, nil
}

func loadGroupModelCounts(groups []string) (map[string]int64, error) {
	rows, err := model.GetEnabledAbilityGroupSummaries(groups)
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int64, len(rows))
	for _, row := range rows {
		counts[row.Group] = row.ModelCount
	}
	return counts, nil
}

func buildGroupStatusEntry(group string, desc string, metrics groupMetrics, modelCount int64, now int64) GroupStatusEntry {
	successRate := roundPercent(metrics.successRate())
	avgLatency := avgInt64(metrics.totalLatencyMs, metrics.requestCount)
	avgTtft := avgInt64(metrics.ttftSumMs, metrics.ttftCount)
	status, confidence, message := classifyGroupHealth(metrics, modelCount, avgLatency, avgTtft, successRate)

	return GroupStatusEntry{
		Group:               group,
		Desc:                desc,
		Status:              status,
		Confidence:          confidence,
		Message:             message,
		RequestCount:        metrics.requestCount,
		SuccessRate:         successRate,
		AvgLatencyMs:        avgLatency,
		AvgTtftMs:           avgTtft,
		AvailableModelCount: modelCount,
		UpdatedAt:           now,
	}
}

func classifyGroupHealth(metrics groupMetrics, modelCount int64, avgLatency int64, avgTtft int64, successRate float64) (string, string, string) {
	if modelCount <= 0 {
		return GroupHealthOutage, groupHealthConfidence(metrics.requestCount), "No routable models are currently enabled for this group."
	}
	if metrics.requestCount < groupHealthMinSamples {
		return GroupHealthUnknown, GroupHealthConfidenceLow, "Not enough recent traffic to determine health."
	}
	if successRate < groupHealthOutageSuccessRate {
		return GroupHealthOutage, groupHealthConfidence(metrics.requestCount), "Recent requests are failing at a high rate."
	}
	if successRate < groupHealthDegradedRate {
		return GroupHealthDegraded, groupHealthConfidence(metrics.requestCount), "Recent success rate is below the healthy threshold."
	}
	if avgLatency >= groupHealthBusyLatencyMs || avgTtft >= groupHealthBusyTtftMs {
		return GroupHealthBusy, groupHealthConfidence(metrics.requestCount), "Requests are succeeding, but latency is elevated."
	}
	return GroupHealthOperational, groupHealthConfidence(metrics.requestCount), "Recent traffic is healthy."
}

func groupHealthConfidence(requestCount int64) string {
	if requestCount >= groupHealthMediumSamples {
		return GroupHealthConfidenceHigh
	}
	return GroupHealthConfidenceMedium
}

func applyAutoGroupMetrics(metrics map[string]groupMetrics, usableGroups map[string]string) {
	if _, ok := usableGroups["auto"]; !ok {
		return
	}
	autoMetrics := metrics["auto"]
	for _, group := range setting.GetAutoGroups() {
		if group == "auto" {
			continue
		}
		if _, ok := usableGroups[group]; !ok {
			continue
		}
		autoMetrics = autoMetrics.add(metrics[group])
	}
	metrics["auto"] = autoMetrics
}

func applyAutoGroupModelCount(modelCounts map[string]int64, usableGroups map[string]string) error {
	if _, ok := usableGroups["auto"]; !ok {
		return nil
	}
	autoGroups := make([]string, 0)
	for _, group := range setting.GetAutoGroups() {
		if _, ok := usableGroups[group]; !ok {
			continue
		}
		autoGroups = append(autoGroups, group)
	}
	autoModelCount, err := model.CountEnabledAbilityModels(autoGroups)
	if err != nil {
		return err
	}
	if autoModelCount > 0 {
		modelCounts["auto"] = autoModelCount
	}
	return nil
}

func (m groupMetrics) successRate() float64 {
	if m.requestCount <= 0 {
		return 0
	}
	return float64(m.successCount) / float64(m.requestCount) * 100
}

func (m groupMetrics) add(other groupMetrics) groupMetrics {
	m.requestCount += other.requestCount
	m.successCount += other.successCount
	m.totalLatencyMs += other.totalLatencyMs
	m.ttftSumMs += other.ttftSumMs
	m.ttftCount += other.ttftCount
	return m
}

func avgInt64(sum int64, count int64) int64 {
	if count <= 0 {
		return 0
	}
	return sum / count
}

func roundPercent(value float64) float64 {
	return math.Round(value*100) / 100
}
