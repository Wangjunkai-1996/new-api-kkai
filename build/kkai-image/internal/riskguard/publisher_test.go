package riskguard

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
)

type recordingRedis struct {
	args *redis.XAddArgs
}

func (client *recordingRedis) XAdd(ctx context.Context, args *redis.XAddArgs) *redis.StringCmd {
	copy := *args
	client.args = &copy
	command := redis.NewStringCmd(ctx)
	command.SetVal("1-0")
	return command
}

func TestRedisPublisherEmitsNewAPICompatibleSignedEnvelope(t *testing.T) {
	client := &recordingRedis{}
	secret := strings.Repeat("s", 32)
	publisher, err := NewRedisPublisher(client, secret)
	if err != nil {
		t.Fatal(err)
	}
	publisher.now = func() time.Time { return time.Unix(1_720_000_000, 0) }
	eventID, err := publisher.Publish(context.Background(), Detection{
		RequestID: "request-1", Model: "gpt-test", RuleID: "pwn_test_rule",
		RuleVersion: "edge_guard_v1", EvidenceSHA256: strings.Repeat("a", 64),
		TokenFingerprint: strings.Repeat("b", 64), BodyBytes: 123,
	})
	if err != nil {
		t.Fatal(err)
	}
	values := client.args.Values.(map[string]any)
	payload := values["payload"].(string)
	signature := values["signature"].(string)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	if client.args.Stream != riskStreamName || signature != hex.EncodeToString(mac.Sum(nil)) {
		t.Fatal("risk stream signature does not bind the exact payload")
	}
	if client.args.MaxLen != riskStreamMaxLen || !client.args.Approx {
		t.Fatalf("risk stream is not bounded: %#v", client.args)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["event_id"] != eventID || decoded["source"] != "edge_guard" || decoded["recommendation"] != "reject" {
		t.Fatalf("unexpected event: %#v", decoded)
	}
	metadata := decoded["metadata"].(map[string]any)
	if metadata["causality"] != "ambiguous" || metadata["upstream_action_allowed"] != false {
		t.Fatalf("unsafe action metadata: %#v", metadata)
	}
	if strings.Contains(payload, "sk-") || strings.Contains(payload, "Authorization") {
		t.Fatal("risk stream payload contains raw credential material")
	}
}
