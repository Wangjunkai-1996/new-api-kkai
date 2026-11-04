package perfmetrics

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKKAIGroupBucketsKeepFullerSnapshotAfterRedisGap(t *testing.T) {
	for _, test := range []struct {
		name           string
		redisRequests  int64
		redisSuccesses int64
		wantRequests   int64
		wantSuccesses  int64
	}{
		{name: "local retains missed success", redisRequests: 1, wantRequests: 2, wantSuccesses: 1},
		{name: "equal Redis snapshot stays authoritative", redisRequests: 2, redisSuccesses: 2, wantRequests: 2, wantSuccesses: 2},
		{name: "larger shared Redis snapshot stays authoritative", redisRequests: 3, redisSuccesses: 2, wantRequests: 3, wantSuccesses: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			resetKKAIGroupSignalState(t)
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			t.Cleanup(func() { require.NoError(t, client.Close()) })
			common.RedisEnabled = true
			common.RDB = client
			observedAt := time.Now().Truncate(time.Minute)
			group := "recovered-group"
			failure := Sample{Model: "test-model", Group: group}
			failureEvent := recordKKAILocalGroupSignal(failure, observedAt)
			recordRedis(bucketKey{model: failure.Model, group: group, bucketTs: bucketStart(observedAt.Unix())}, failure, observedAt, failureEvent)

			// A successful request is retained locally when its Redis write fails.
			recordKKAILocalGroupSignal(Sample{Model: "test-model", Group: group, Success: true}, observedAt.Add(time.Second))
			for _, bucket := range []struct {
				prefix     string
				resolution int64
			}{
				{prefix: "minute", resolution: kkaiGroupMinuteSeconds},
				{prefix: "historical-5m", resolution: kkaiGroupHistoricalSeconds},
			} {
				key := kkaiGroupRedisBucketKey(group, bucket.prefix, observedAt.Unix()-observedAt.Unix()%bucket.resolution)
				require.NoError(t, client.HSet(context.Background(), key, "req", test.redisRequests, "ok", test.redisSuccesses).Err())
			}

			for _, result := range []KKAIGroupBucketResult{
				QueryKKAIGroupMinuteBuckets(observedAt.Unix(), observedAt.Add(time.Second).Unix(), []string{group}),
				QueryKKAIGroupHistoricalBuckets(observedAt.Unix(), observedAt.Add(time.Second).Unix(), []string{group}),
			} {
				assert.True(t, result.RedisAvailable)
				require.Len(t, result.Buckets, 1)
				assert.Equal(t, test.wantRequests, result.Buckets[0].RequestCount)
				assert.Equal(t, test.wantSuccesses, result.Buckets[0].SuccessCount)
			}
		})
	}
}
