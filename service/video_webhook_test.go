package service

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestVideoWebhookSignatureAndChallenge(t *testing.T) {
	body := []byte(`{"event_type":"webhook.challenge","challenge":"demo-01"}`)
	assert.Equal(t, "v1=3102bea5596271d001d40b8c26b45614ab1d0e901b3a89aa36a44046ef45bff1", videoWebhookSignature("demo-key-for-offline-use", "1795651242", body))
	setVideoWebhookTestKeys(t)
	ep := model.VideoWebhookEndpoint{ID: "wh_demo", URL: "https://example.test/notify", KeyVersion: "new"}
	secret, err := VideoWebhookSecret(ep)
	require.NoError(t, err)
	old := ep
	old.KeyVersion = "old"
	oldSecret, err := VideoWebhookSecret(old)
	require.NoError(t, err)
	assert.NotEqual(t, secret, oldSecret)

	client := &http.Client{Transport: videoArchiveRoundTripper(func(r *http.Request) (*http.Response, error) {
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Equal(t, ep.ID, r.Header.Get("X-KKRICH-Webhook-Key-Id"))
		assert.Equal(t, videoWebhookSignature(secret, r.Header.Get("X-KKRICH-Webhook-Timestamp"), raw), r.Header.Get("X-KKRICH-Webhook-Signature"))
		assert.Empty(t, r.Header.Get("Authorization"))
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"challenge":"demo-01"}`))}, nil
	})}
	require.NoError(t, sendVideoWebhook(t.Context(), client, ep, body, "demo-01"))
	require.ErrorIs(t, sendVideoWebhook(t.Context(), client, ep, body, "new-challenge"), ErrVideoWebhookChallenge)
	for _, raw := range []string{"http://example.test/cb", "https://127.0.0.1/cb", "https://[::1]/cb", "https://169.254.169.254/cb", "https://8.8.8.8:444/cb", "https://user@example.test/", "https://8.8.8.8/cb?key=x", "https://8.8.8.8/cb#x"} {
		assert.ErrorIs(t, ValidateVideoWebhookURL(t.Context(), raw), ErrVideoWebhookURL)
	}
	assert.NoError(t, ValidateVideoWebhookURL(t.Context(), "https://8.8.8.8/notify"))
}

func TestVideoWebhookLifecycle(t *testing.T) {
	ctx := t.Context()
	db := newOutboxTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Token{}, &model.User{}, &model.KKAIVideoGeneration{}, &model.KKAIIdempotencyKey{}))
	require.NoError(t, kkaimigrate.ApplyVideoWebhookExpansion(ctx, db, kkaimigrate.Options{Offline: true}))
	setVideoWebhookTestKeys(t)
	t.Setenv("KKAI_VIDEO_WEBHOOK_ENABLED", "true")
	previousOrigin := system_setting.ServerAddress
	t.Cleanup(func() { system_setting.ServerAddress = previousOrigin })
	system_setting.ServerAddress = "https://gateway.example.test/"
	user := model.User{Username: "hookowner", AffCode: "hookowner", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	token := model.Token{UserId: user.Id, Key: "api-token-fixture", Status: common.TokenStatusEnabled}
	require.NoError(t, db.Create(&token).Error)
	owner := model.VideoWebhookOwner{UserID: user.Id, TokenID: token.Id}
	ep := model.VideoWebhookEndpoint{ID: NewVideoWebhookID("wh_"), VideoWebhookOwner: owner, URL: "https://example.test/callback", State: "pending", KeyVersion: "old"}
	require.NoError(t, owner.ChangeEndpoint(ctx, db, "create", "", &ep))
	require.NoError(t, owner.ChangeEndpoint(ctx, db, "verify", ep.ID, nil))
	task := model.Task{
		TaskID: "task_fixture", UserId: user.Id, Action: constant.TaskActionTextToVideo,
		Status: model.TaskStatusUnknown, PrivateData: model.TaskPrivateData{TokenId: token.Id, BillingState: model.TaskBillingStateAmbiguous},
	}
	eventID := NewVideoWebhookID("evt_")
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&task).Error; err != nil {
			return err
		}
		return owner.Subscribe(tx, &task, ep.ID, eventID, "order-31", "sd_2.5_special_720p")
	}))
	now := time.Now()
	var sub model.VideoWebhookTask
	require.NoError(t, db.First(&sub, "task_row_id = ?", task.ID).Error)
	require.NoError(t, observeVideoWebhook(ctx, db, sub, now))
	var events int64
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("topic = ?", VideoWebhookTopic).Count(&events).Error)
	require.Zero(t, events)

	replacement := model.VideoWebhookEndpoint{ID: NewVideoWebhookID("wh_"), VideoWebhookOwner: owner, URL: "https://example.test/new-callback", State: "pending", KeyVersion: "new"}
	require.NoError(t, owner.ChangeEndpoint(ctx, db, "create", "", &replacement))
	require.NoError(t, owner.ChangeEndpoint(ctx, db, "verify", replacement.ID, nil))
	var scoped int64
	require.NoError(t, (model.VideoWebhookOwner{UserID: user.Id + 1, TokenID: token.Id}).Scope(db.Model(&model.VideoWebhookEndpoint{})).Count(&scoped).Error)
	assert.Zero(t, scoped)
	now = now.Add(6 * time.Minute)
	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = oldDB })
	recovered, ok, err := model.RecoverTaskSubmissionAcceptance(ctx, task.ID, model.TaskSubmissionAcceptance{
		UpstreamTaskID: "hidden-provider-task", ResultURL: "https://provider.example.test/video?token=hidden",
		Status: model.TaskStatusSuccess, FinishTime: now.Unix(),
	})
	require.NoError(t, err)
	require.True(t, ok)
	task = *recovered
	require.NoError(t, observeVideoWebhook(ctx, db, sub, now))
	require.NoError(t, observeVideoWebhook(ctx, db, sub, now))
	var event model.KKAIOutboxEvent
	require.NoError(t, db.Where("topic = ?", VideoWebhookTopic).First(&event).Error)
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("topic = ?", VideoWebhookTopic).Count(&events).Error)
	require.EqualValues(t, 1, events)
	assert.Equal(t, VideoWebhookTopic, event.Topic)
	want := videoWebhookEvent{Version: 1, EventID: eventID, EventType: "video.completed", TaskID: task.TaskID, Reference: "order-31", Model: "sd_2.5_special_720p", Status: model.TaskStatusSuccess, FinishedAt: task.FinishTime, ResultURL: "https://gateway.example.test/v1/videos/task_fixture/content"}
	expected, err := common.Marshal(want)
	require.NoError(t, err)
	assert.JSONEq(t, string(expected), event.Payload)
	assert.NotContains(t, event.Payload, "hidden")
	require.NoError(t, db.Model(&token).Update("status", common.TokenStatusExhausted).Error)
	secret, err := VideoWebhookSecret(ep)
	require.NoError(t, err)
	status := 429
	attempts := 0
	client := &http.Client{Transport: videoArchiveRoundTripper(func(req *http.Request) (*http.Response, error) {
		attempts++
		raw, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		assert.Equal(t, event.Payload, string(raw))
		assert.Equal(t, ep.URL, req.URL.String())
		assert.Equal(t, videoWebhookSignature(secret, req.Header.Get("X-KKRICH-Webhook-Timestamp"), raw), req.Header.Get("X-KKRICH-Webhook-Signature"))
		return &http.Response{StatusCode: status, Body: http.NoBody}, nil
	})}
	processor := NewKKAIOutboxProcessor(db, "video-webhook-test")
	processor.now = func() time.Time { return now }
	require.NoError(t, processor.Register(VideoWebhookTopic, func(ctx context.Context, e model.KKAIOutboxEvent) error {
		endpoint, err := videoWebhookRecipient(ctx, db, e)
		if err != nil {
			return err
		}
		return sendVideoWebhook(ctx, client, endpoint, []byte(e.Payload), "")
	}))
	batch, err := processor.ProcessBatch(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, batch.Retried)
	now = now.Add(time.Minute)
	status = 302
	batch, err = processor.ProcessBatch(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, batch.Dead)
	key := "support-1"
	applied, err := RedriveVideoWebhook(ctx, db, eventID, key, "fixture", now)
	require.NoError(t, err)
	assert.True(t, applied)
	applied, err = RedriveVideoWebhook(ctx, db, eventID, key, "fixture", now)
	require.NoError(t, err)
	assert.False(t, applied)
	// Two support tickets can each redrive a broken endpoint. Replaying the
	// first ticket after the second failed must still be a no-op.
	batch, err = processor.ProcessBatch(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, 1, batch.Dead)
	applied, err = RedriveVideoWebhook(ctx, db, eventID, "support-2", "fixture", now)
	require.NoError(t, err)
	require.True(t, applied)
	batch, err = processor.ProcessBatch(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, 1, batch.Dead)
	applied, err = RedriveVideoWebhook(ctx, db, eventID, key, "fixture", now)
	require.NoError(t, err)
	assert.False(t, applied)
	applied, err = RedriveVideoWebhook(ctx, db, eventID, "support-3", "fixture", now)
	require.NoError(t, err)
	require.True(t, applied)
	status = 204
	batch, err = processor.ProcessBatch(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, batch.Delivered)
	assert.Equal(t, 5, attempts)
	require.NoError(t, db.First(&sub, "task_row_id = ?", task.ID).Error)
	deliveries, err := VideoWebhookDeliveries(db, []model.VideoWebhookTask{sub})
	require.NoError(t, err)
	require.Len(t, deliveries, 1)
	assert.Equal(t, model.KKAIOutboxStatusDelivered, deliveries[0].DeliveryStatus)
	assert.NotContains(t, deliveries[0].LastError, "fixture")
	require.NoError(t, owner.ChangeEndpoint(ctx, db, "revoke", ep.ID, nil))
	_, err = videoWebhookRecipient(ctx, db, event)
	var permanent permanentKKAIOutboxError
	assert.True(t, errors.As(err, &permanent))
	assert.ErrorContains(t, err, "webhook_revoked")
	require.NoError(t, cleanupVideoWebhooks(ctx, db, now.Add(31*24*time.Hour).Unix()))
	require.ErrorIs(t, db.First(&sub, "task_row_id = ?", task.ID).Error, gorm.ErrRecordNotFound)
	require.ErrorIs(t, db.First(&ep, "id = ?", ep.ID).Error, gorm.ErrRecordNotFound)
}

func TestVideoWebhookAcceptedFailureOnly(t *testing.T) {
	db := newOutboxTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.User{}, &model.Token{}))
	require.NoError(t, kkaimigrate.ApplyVideoWebhookExpansion(t.Context(), db, kkaimigrate.Options{Offline: true}))
	user := model.User{Username: "failedtaskowner", AffCode: "failedtaskowner"}
	require.NoError(t, db.Create(&user).Error)
	token := model.Token{UserId: user.Id, Key: "failedtoken"}
	require.NoError(t, db.Create(&token).Error)
	owner := model.VideoWebhookOwner{UserID: user.Id, TokenID: token.Id}
	ep := model.VideoWebhookEndpoint{ID: "wh_failure", VideoWebhookOwner: owner, State: "active"}
	require.NoError(t, db.Create(&ep).Error)
	now := time.Now()
	for _, upstreamID := range []string{"", "provider-id"} {
		task := model.Task{TaskID: "task_" + upstreamID, UserId: user.Id, Status: model.TaskStatusFailure, FinishTime: now.Unix(),
			PrivateData: model.TaskPrivateData{TokenId: token.Id, UpstreamTaskID: upstreamID, Failure: &model.TaskFailureSnapshot{Code: "content_policy_violation", Message: "provider-private message"}}}
		require.NoError(t, db.Create(&task).Error)
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
			return owner.Subscribe(tx, &task, ep.ID, "evt_"+upstreamID, "", "seedance-2.5")
		}))
		var sub model.VideoWebhookTask
		require.NoError(t, db.First(&sub, "task_row_id = ?", task.ID).Error)
		require.NoError(t, observeVideoWebhook(t.Context(), db, sub, now.Add(time.Second)))
		var event model.KKAIOutboxEvent
		err := db.Where("aggregate_id = ?", strconv.FormatInt(task.ID, 10)).First(&event).Error
		if upstreamID == "" {
			assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
			continue
		}
		require.NoError(t, err)
		var body videoWebhookEvent
		require.NoError(t, common.UnmarshalJsonStr(event.Payload, &body))
		assert.Equal(t, "video.failed", body.EventType)
		assert.Equal(t, "content_policy_violation", body.ErrorCode)
		assert.Empty(t, body.ResultURL)
		assert.NotContains(t, event.Payload, "provider-private")
	}
}

func setVideoWebhookTestKeys(t *testing.T) {
	t.Helper()
	encoded, err := common.Marshal(map[string]string{
		"old": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32))),
		"new": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("b", 32))),
	})
	require.NoError(t, err)
	t.Setenv("KKAI_VIDEO_WEBHOOK_KEYS", string(encoded))
	t.Setenv("KKAI_VIDEO_WEBHOOK_KEYS_FILE", "")
}

func TestVideoWebhookRegisteredJobsDeliverOverVerifiedTLS(t *testing.T) {
	ctx := t.Context()
	db := newOutboxTestDB(t)
	oldDB, oldMode, oldUnavailable := model.DB, common.SchemaManagementMode, videoWebhookSchemaUnavailable.Load()
	model.DB, common.SchemaManagementMode = db, common.SchemaManagementExternal
	t.Cleanup(func() {
		model.DB, common.SchemaManagementMode = oldDB, oldMode
		videoWebhookSchemaUnavailable.Store(oldUnavailable)
	})
	t.Setenv("KKAI_VIDEO_WEBHOOK_ENABLED", "true")
	t.Setenv("KKAI_VIDEO_WEBHOOK_TOKENS", "*")
	registry := NewBackgroundJobRegistry()
	require.NoError(t, registerVideoWebhookJobs(registry, "fixture"))
	assert.False(t, VideoWebhookEnabled(), "missing optional schema must disable only webhook")
	require.NoError(t, kkaimigrate.ApplyVideoWebhookExpansion(ctx, db, kkaimigrate.Options{Offline: true}))
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Task{}))
	require.NoError(t, registerVideoWebhookJobs(registry, "fixture"))
	require.True(t, VideoWebhookEnabled())
	user := model.User{Username: "workerfixture", AffCode: "workerfixture"}
	require.NoError(t, db.Create(&user).Error)
	token := model.Token{UserId: user.Id, Key: "workerfixture", Status: common.TokenStatusEnabled}
	require.NoError(t, db.Create(&token).Error)
	owner := model.VideoWebhookOwner{UserID: user.Id, TokenID: token.Id}
	ep := model.VideoWebhookEndpoint{ID: "wh_worker", VideoWebhookOwner: owner, URL: "https://example.com/receiver", State: "pending", KeyVersion: "old"}
	setVideoWebhookTestKeys(t)
	secret, err := VideoWebhookSecret(ep)
	require.NoError(t, err)
	delivered := make(chan string, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil || r.Host != "example.com" || r.Header.Get("X-KKRICH-Webhook-Key-Id") != ep.ID || r.Header.Get("X-KKRICH-Webhook-Signature") != videoWebhookSignature(secret, r.Header.Get("X-KKRICH-Webhook-Timestamp"), body) {
			w.WriteHeader(403)
			return
		}
		var event map[string]any
		if common.Unmarshal(body, &event) != nil {
			w.WriteHeader(400)
			return
		}
		if event["event_type"] == "webhook.challenge" {
			response, _ := common.Marshal(map[string]any{"challenge": event["challenge"]})
			_, _ = w.Write(response)
			return
		}
		delivered <- string(body)
		w.WriteHeader(204)
	}))
	t.Cleanup(server.Close)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	oldClient := videoWebhookClient
	t.Cleanup(func() { videoWebhookClient = oldClient })
	client := *oldClient
	client.Transport = &videoArchivePinnedRoundTripper{
		resolver:      staticSSRFResolver{"example.com": {{IP: net.ParseIP("8.8.8.8")}}},
		baseTLSConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
		dialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if addr != "8.8.8.8:443" {
				return nil, errors.New("dial target was not pinned")
			}
			return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
		},
	}
	videoWebhookClient = &client
	require.NoError(t, owner.ChangeEndpoint(ctx, db, "create", "", &ep))
	require.NoError(t, VerifyVideoWebhook(ctx, ep))
	require.NoError(t, owner.ChangeEndpoint(ctx, db, "verify", ep.ID, nil))
	task := model.Task{TaskID: "task_worker", UserId: user.Id, Status: model.TaskStatusFailure, FinishTime: time.Now().Unix(), PrivateData: model.TaskPrivateData{TokenId: token.Id, UpstreamTaskID: "provider"}}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&task).Error; err != nil {
			return err
		}
		return owner.Subscribe(tx, &task, ep.ID, "evt_worker", "", "seedance-2.5")
	}))
	require.NoError(t, registry.jobs["video-webhook-observe"].Run(ctx))
	t.Setenv("KKAI_VIDEO_WEBHOOK_PAUSE_DELIVERY", "true")
	require.NoError(t, registry.jobs["video-webhook-deliver"].Run(ctx))
	require.Empty(t, delivered)
	t.Setenv("KKAI_VIDEO_WEBHOOK_PAUSE_DELIVERY", "false")
	require.NoError(t, registry.jobs["video-webhook-deliver"].Run(ctx))
	select {
	case body := <-delivered:
		assert.Contains(t, body, `"event_id":"evt_worker"`)
	default:
		t.Fatal("no delivery from the registered worker")
	}
	var event model.KKAIOutboxEvent
	require.NoError(t, db.Where("topic = ?", VideoWebhookTopic).First(&event).Error)
	assert.Equal(t, model.KKAIOutboxStatusDelivered, event.Status)
}
