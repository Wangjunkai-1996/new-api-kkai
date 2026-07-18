package riskguard

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestRedisPublisherWithRestrictedACL(t *testing.T) {
	address := os.Getenv("KKAI_TEST_RISK_REDIS_ADDRESS")
	password := os.Getenv("KKAI_TEST_RISK_REDIS_PASSWORD")
	if address == "" || password == "" {
		t.Skip("restricted Redis integration environment is not configured")
	}
	client := redis.NewClient(&redis.Options{
		Addr: address, Username: "risk", Password: password,
	})
	t.Cleanup(func() { _ = client.Close() })
	publisher, err := NewRedisPublisher(client, strings.Repeat("s", 32))
	if err != nil {
		t.Fatal(err)
	}
	_, err = publisher.Publish(context.Background(), Detection{
		RuleID: "acl_integration", RuleVersion: "edge_guard_v1",
		EvidenceSHA256: strings.Repeat("a", 64), BodyBytes: 64,
	})
	if err != nil {
		t.Fatalf("restricted publisher failed: %v", err)
	}
}
