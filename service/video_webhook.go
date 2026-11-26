package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

const VideoWebhookTopic = "video.webhook.terminal.v1"
const VideoWebhookHeader = "X-KKRICH-Webhook"
const VideoWebhookReferenceHeader = "X-KKRICH-Client-Reference"

var ErrVideoWebhookUnavailable = errors.New("webhook_unavailable")
var ErrVideoWebhookURL = errors.New("invalid_webhook_url")
var ErrVideoWebhookChallenge = errors.New("webhook_verification_failed")

var videoWebhookSchemaUnavailable atomic.Bool

func VideoWebhookEnabled() bool {
	return os.Getenv("KKAI_VIDEO_WEBHOOK_ENABLED") == "true" && !videoWebhookSchemaUnavailable.Load()
}
func VideoWebhookMayRegister(tokenID int) bool {
	_, err := videoWebhookDownloadOrigin()
	return err == nil && VideoWebhookTokenEnabled(tokenID) && os.Getenv("KKAI_VIDEO_WEBHOOK_PAUSE_REGISTRATION") != "true"
}

func VideoWebhookTokenEnabled(tokenID int) bool {
	if !VideoWebhookEnabled() || tokenID <= 0 {
		return false
	}
	allowed := strings.TrimSpace(os.Getenv("KKAI_VIDEO_WEBHOOK_TOKENS"))
	if allowed == "*" {
		return true
	}
	for _, entry := range strings.Split(allowed, ",") {
		if strings.TrimSpace(entry) == strconv.Itoa(tokenID) {
			return true
		}
	}
	return false
}

func videoWebhookDownloadOrigin() (*url.URL, error) {
	base, err := url.Parse(system_setting.ServerAddress)
	if err != nil || base.Scheme != "https" || base.Hostname() == "" || base.User != nil ||
		(base.Path != "" && base.Path != "/") || strings.ContainsAny(system_setting.ServerAddress, "?#") {
		return nil, errors.New("invalid public video download origin")
	}
	return base, nil
}

func NewVideoWebhookID(prefix string) string {
	var b [24]byte
	rand.Read(b[:])
	return prefix + base64.RawURLEncoding.EncodeToString(b[:])
}

// A unique, immutable endpoint version is the salt for its signing secret.
// Only key versions are stored in the database. Rotation can retain old master keys.
func VideoWebhookSecret(endpoint model.VideoWebhookEndpoint) (string, error) {
	raw, err := readMountedSecret("KKAI_VIDEO_WEBHOOK_KEYS")
	var keys map[string]string
	if err != nil || common.UnmarshalJsonStr(raw, &keys) != nil || endpoint.KeyVersion == "" {
		return "", ErrVideoWebhookUnavailable
	}
	key, err := base64.StdEncoding.DecodeString(keys[endpoint.KeyVersion])
	if err != nil || len(key) != 32 {
		return "", ErrVideoWebhookUnavailable
	}
	mac := hmac.New(sha256.New, key)
	fmt.Fprintf(mac, "kkai.video.webhook.v1\x00%d\x00%d\x00%d\x00%s\x00%s", endpoint.TenantID, endpoint.UserID, endpoint.TokenID, endpoint.ID, endpoint.URL)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func ValidateVideoWebhookURL(ctx context.Context, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || strings.ContainsAny(raw, "?#") || u.Opaque != "" {
		return ErrVideoWebhookURL
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err = resolveStrictVideoArchiveTarget(ctx, u, net.DefaultResolver); err != nil {
		return ErrVideoWebhookURL
	}
	return nil
}

var videoWebhookClient = &http.Client{
	Timeout:       5 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	Transport:     &videoArchivePinnedRoundTripper{resolver: net.DefaultResolver, dialContext: (&net.Dialer{Timeout: 4 * time.Second}).DialContext},
}

func videoWebhookSignature(secret, timestamp string, body []byte) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(timestamp + "."))
	h.Write(body)
	return "v1=" + hex.EncodeToString(h.Sum(nil))
}

func sendVideoWebhook(ctx context.Context, client *http.Client, ep model.VideoWebhookEndpoint, body []byte, challenge string) error {
	secret, err := VideoWebhookSecret(ep)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.URL, bytes.NewReader(body))
	if err != nil || strings.ContainsAny(ep.URL, "?#") {
		return PermanentKKAIOutboxError(ErrVideoWebhookURL)
	}
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-KKRICH-Webhook-Key-Id", ep.ID)
	req.Header.Set("X-KKRICH-Webhook-Timestamp", stamp)
	req.Header.Set("X-KKRICH-Webhook-Signature", videoWebhookSignature(secret, stamp, body))
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, ErrVideoArchiveSourceRejected) {
			return PermanentKKAIOutboxError(ErrVideoWebhookURL)
		}
		var certificateError *tls.CertificateVerificationError
		if errors.As(err, &certificateError) {
			return PermanentKKAIOutboxError(errors.New("webhook_tls_verification_failed"))
		}
		return errors.New("webhook_connection_failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		failure := fmt.Errorf("webhook_http_%d", resp.StatusCode)
		if resp.StatusCode >= 500 || resp.StatusCode == 408 || resp.StatusCode == 429 {
			return failure
		}
		return PermanentKKAIOutboxError(failure)
	}
	if challenge != "" {
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 1025))
		var reply map[string]string
		if err != nil || len(raw) > 1024 || common.Unmarshal(raw, &reply) != nil || len(reply) != 1 || reply["challenge"] != challenge {
			return ErrVideoWebhookChallenge
		}
	}
	return nil
}

func VerifyVideoWebhook(ctx context.Context, ep model.VideoWebhookEndpoint) error {
	challenge := NewVideoWebhookID("")
	body, err := common.Marshal(map[string]string{"event_type": "webhook.challenge", "challenge": challenge})
	if err != nil {
		return err
	}
	return sendVideoWebhook(ctx, videoWebhookClient, ep, body, challenge)
}
