package model

type KKAIPerfMetricBucket struct {
	Group          string `json:"group"`
	BucketTs       int64  `json:"bucket_ts"`
	RequestCount   int64  `json:"request_count"`
	SuccessCount   int64  `json:"success_count"`
	TotalLatencyMs int64  `json:"total_latency_ms"`
	TtftSumMs      int64  `json:"ttft_sum_ms"`
	TtftCount      int64  `json:"ttft_count"`
}

func GetKKAIPerfMetricBuckets(startTs int64, endTs int64, groups []string) ([]KKAIPerfMetricBucket, error) {
	var buckets []KKAIPerfMetricBucket
	if len(groups) == 0 || startTs <= 0 || endTs < startTs {
		return buckets, nil
	}
	err := DB.Model(&PerfMetric{}).
		Select(commonGroupCol+", bucket_ts, SUM(request_count) AS request_count, SUM(success_count) AS success_count, SUM(total_latency_ms) AS total_latency_ms, SUM(ttft_sum_ms) AS ttft_sum_ms, SUM(ttft_count) AS ttft_count").
		Where("bucket_ts >= ? AND bucket_ts <= ?", startTs, endTs).
		Where(commonGroupCol+" IN ?", groups).
		Group(commonGroupCol + ", bucket_ts").
		Order("bucket_ts ASC").
		Find(&buckets).Error
	return buckets, err
}
