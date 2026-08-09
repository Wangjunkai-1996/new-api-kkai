package service

import (
	"sort"
	"time"

	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
)

func GetKKAIGroupStatuses(request KKAIGroupStatusRequest) (KKAIGroupStatusResult, error) {
	now := kkaiGroupStatusNow()
	window := normalizeKKAIGroupStatusWindow(request)
	groups := sortedKKAIGroupNames(request.UsableGroups)
	if len(groups) == 0 {
		return KKAIGroupStatusResult{
			GeneratedAt:   now.Unix(),
			Window:        window.name,
			WindowMinutes: window.minutes,
			WindowHours:   window.compatHours,
			DataSource:    perfmetrics.KKAIGroupDataSourceNone,
			Groups:        []KKAIGroupStatusEntry{},
		}, nil
	}

	startTs := now.Add(-time.Duration(window.minutes) * time.Minute).Unix()
	endTs := now.Unix()
	signals := queryKKAIGroupRecentSignals(groups, kkaiGroupRecentEventLimit)
	metrics := make(map[string]kkaiGroupMetrics, len(groups))
	dataSource := perfmetrics.KKAIGroupDataSourceNone
	redisAvailable := signals.RedisAvailable
	if window.minutes <= 15 {
		buckets := queryKKAIGroupMinuteBuckets(startTs, endTs, groups)
		mergeKKAIPerfBuckets(metrics, buckets.Buckets)
		dataSource = buckets.Source
		redisAvailable = buckets.RedisAvailable && signals.RedisAvailable
	} else {
		databaseBuckets, err := loadKKAIPerfMetricBuckets(startTs, endTs, groups)
		if err != nil {
			return KKAIGroupStatusResult{}, err
		}
		hourly := queryKKAIGroupHourBuckets(startTs, endTs, groups)
		metrics = mergeKKAIDatabaseAndLiveBuckets(databaseBuckets, hourly.Buckets)
		dataSource = combinedKKAIGroupDataSource(len(databaseBuckets) > 0, hourly.Source)
		redisAvailable = hourly.RedisAvailable && signals.RedisAvailable
	}

	applyKKAIAutoGroupMetrics(metrics, request.UsableGroups, request.AutoGroups)
	eventsByGroup := kkaiGroupRecentEventsByGroup(signals.Events, kkaiGroupRecentEventLimit)
	applyKKAIAutoGroupEvents(eventsByGroup, request.UsableGroups, request.AutoGroups, kkaiGroupRecentEventLimit)
	entries := make([]KKAIGroupStatusEntry, 0, len(groups))
	for _, group := range groups {
		recentEvents := eventsByGroup[group]
		if recentEvents == nil {
			recentEvents = []KKAIGroupRecentEvent{}
		}
		entries = append(entries, buildKKAIGroupStatusEntry(
			group,
			request.UsableGroups[group],
			metrics[group],
			now,
			window,
			dataSource,
			recentEvents,
		))
	}
	return KKAIGroupStatusResult{
		GeneratedAt:    now.Unix(),
		Window:         window.name,
		WindowMinutes:  window.minutes,
		WindowHours:    window.compatHours,
		DataSource:     dataSource,
		RedisAvailable: redisAvailable,
		Groups:         entries,
	}, nil
}

func normalizeKKAIGroupStatusWindow(request KKAIGroupStatusRequest) kkaiGroupStatusWindow {
	switch request.Window {
	case "now", "realtime", "live":
		return kkaiGroupStatusWindow{name: "now", minutes: 5, compatHours: 1, live: true, staleAfter: 2 * time.Minute}
	case "15m":
		return kkaiGroupStatusWindow{name: "15m", minutes: 15, compatHours: 1, staleAfter: 5 * time.Minute}
	case "1h":
		return kkaiGroupStatusWindow{name: "1h", minutes: 60, compatHours: 1, staleAfter: 15 * time.Minute}
	case "6h":
		return kkaiGroupStatusWindow{name: "6h", minutes: 360, compatHours: 6, staleAfter: 2 * time.Hour}
	case "24h":
		return kkaiGroupStatusWindow{name: "24h", minutes: 1440, compatHours: 24, staleAfter: 6 * time.Hour}
	}
	switch request.Hours {
	case 1:
		return kkaiGroupStatusWindow{name: "1h", minutes: 60, compatHours: 1, staleAfter: 15 * time.Minute}
	case 6:
		return kkaiGroupStatusWindow{name: "6h", minutes: 360, compatHours: 6, staleAfter: 2 * time.Hour}
	case 24:
		return kkaiGroupStatusWindow{name: "24h", minutes: 1440, compatHours: 24, staleAfter: 6 * time.Hour}
	default:
		return kkaiGroupStatusWindow{name: "now", minutes: 5, compatHours: 1, live: true, staleAfter: 2 * time.Minute}
	}
}

func sortedKKAIGroupNames(groups map[string]string) []string {
	names := make([]string, 0, len(groups))
	for group := range groups {
		names = append(names, group)
	}
	sort.Strings(names)
	return names
}
