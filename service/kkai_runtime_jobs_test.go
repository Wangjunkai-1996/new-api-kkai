package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestKKAIOutboxRuntimeJobIsolatesSlowWebhookFromRiskAndBilling(t *testing.T) {
	db := newOutboxTestDB(t)
	now := time.Unix(1_720_000_000, 0)
	webhookEvent := seedOutboxEvent(t, db, model.KKAIOutboxTopicTopUpCompleted, now.Unix())
	riskEvent := seedOutboxEvent(t, db, KKAIOutboxTopicRiskActionCommitted, now.Unix())
	billingEvent := seedOutboxEvent(t, db, model.KKAIOutboxTopicTaskBillingAudit, now.Unix())

	webhookStarted := make(chan struct{})
	releaseWebhook := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-releaseWebhook:
		default:
			close(releaseWebhook)
		}
	})
	riskDelivered := make(chan struct{})
	billingDelivered := make(chan struct{})
	noopHandler := func(context.Context, model.KKAIOutboxEvent) error { return nil }
	worker, err := newKKAIOutboxRuntimeWorker(db, "runtime-worker", kkaiOutboxRuntimeHandlers{
		taskBillingAudit: func(context.Context, model.KKAIOutboxEvent) error {
			close(billingDelivered)
			return nil
		},
		taskBillingCacheReconcile: noopHandler,
		taskBillingRecovery:       noopHandler,
		taskAccounting:            noopHandler,
		riskActionCommitted: func(context.Context, model.KKAIOutboxEvent) error {
			close(riskDelivered)
			return nil
		},
		topUpCompleted: func(context.Context, model.KKAIOutboxEvent) error {
			close(webhookStarted)
			<-releaseWebhook
			return nil
		},
	})
	require.NoError(t, err)
	require.Len(t, worker.processors, 3)
	for _, processor := range worker.processors {
		processor.now = func() time.Time { return now }
	}

	registry := NewBackgroundJobRegistry()
	require.NoError(t, registerKKAIOutboxDeliveryJob(registry, worker))
	descriptors := registry.Descriptors()
	require.Len(t, descriptors, 1)
	require.Equal(t, "kkai-outbox-delivery", descriptors[0].Name)
	require.True(t, descriptors[0].WritesData)
	require.True(t, descriptors[0].RequiresLeaderLease)

	done := make(chan error, 1)
	go func() {
		done <- worker.ProcessOnce(context.Background())
	}()
	<-webhookStarted

	select {
	case <-riskDelivered:
	case <-time.After(time.Second):
		t.Fatal("risk outbox lane was blocked by the webhook handler")
	}
	select {
	case <-billingDelivered:
	case <-time.After(time.Second):
		t.Fatal("billing outbox lane was blocked by the webhook handler")
	}
	require.Eventually(t, func() bool {
		return db.First(&riskEvent, riskEvent.ID).Error == nil && riskEvent.Status == model.KKAIOutboxStatusDelivered
	}, time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool {
		return db.First(&billingEvent, billingEvent.ID).Error == nil && billingEvent.Status == model.KKAIOutboxStatusDelivered
	}, time.Second, 5*time.Millisecond)
	require.NoError(t, db.First(&webhookEvent, webhookEvent.ID).Error)
	require.Equal(t, model.KKAIOutboxStatusPending, webhookEvent.Status)
	require.NotEmpty(t, webhookEvent.LockedBy)

	close(releaseWebhook)
	require.NoError(t, <-done)
	require.NoError(t, db.First(&webhookEvent, webhookEvent.ID).Error)
	require.Equal(t, model.KKAIOutboxStatusDelivered, webhookEvent.Status)
	require.Empty(t, webhookEvent.LockedBy)
}

func TestKKAIOutboxRuntimeWorkerRetriesHandlerPanicWithoutEscapingWorker(t *testing.T) {
	db := newOutboxTestDB(t)
	now := time.Unix(1_720_000_000, 0)
	event := seedOutboxEvent(t, db, KKAIOutboxTopicRiskActionCommitted, now.Unix())
	noopHandler := func(context.Context, model.KKAIOutboxEvent) error { return nil }
	worker, err := newKKAIOutboxRuntimeWorker(db, "runtime-worker", kkaiOutboxRuntimeHandlers{
		taskBillingAudit:          noopHandler,
		taskBillingCacheReconcile: noopHandler,
		taskBillingRecovery:       noopHandler,
		taskAccounting:            noopHandler,
		riskActionCommitted: func(context.Context, model.KKAIOutboxEvent) error {
			panic("lane failure")
		},
	})
	require.NoError(t, err)
	for _, processor := range worker.processors {
		processor.now = func() time.Time { return now }
	}

	err = worker.ProcessOnce(context.Background())
	require.NoError(t, err)
	require.NoError(t, db.First(&event, event.ID).Error)
	require.Equal(t, model.KKAIOutboxStatusPending, event.Status)
	require.Equal(t, 1, event.Attempts)
	require.Equal(t, now.Add(5*time.Second).Unix(), event.AvailableAt)
	require.Empty(t, event.LockedBy)
	require.Contains(t, event.LastError, "KKAI outbox handler panic: lane failure")
}

func TestKKAIOutboxRuntimeWorkerCancellationStopsAllLanes(t *testing.T) {
	db := newOutboxTestDB(t)
	now := time.Unix(1_720_000_000, 0)
	events := []model.KKAIOutboxEvent{
		seedOutboxEvent(t, db, model.KKAIOutboxTopicTaskBillingAudit, now.Unix()),
		seedOutboxEvent(t, db, KKAIOutboxTopicRiskActionCommitted, now.Unix()),
		seedOutboxEvent(t, db, model.KKAIOutboxTopicTopUpCompleted, now.Unix()),
	}

	started := make(chan string, len(events))
	blockingHandler := func(ctx context.Context, event model.KKAIOutboxEvent) error {
		started <- event.Topic
		<-ctx.Done()
		return ctx.Err()
	}
	noopHandler := func(context.Context, model.KKAIOutboxEvent) error { return nil }
	worker, err := newKKAIOutboxRuntimeWorker(db, "runtime-worker", kkaiOutboxRuntimeHandlers{
		taskBillingAudit:          blockingHandler,
		taskBillingCacheReconcile: noopHandler,
		taskBillingRecovery:       noopHandler,
		taskAccounting:            noopHandler,
		riskActionCommitted:       blockingHandler,
		topUpCompleted:            blockingHandler,
	})
	require.NoError(t, err)
	for _, processor := range worker.processors {
		processor.now = func() time.Time { return now }
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- worker.ProcessOnce(ctx)
	}()
	startedTopics := make(map[string]bool, len(events))
	for range events {
		select {
		case topic := <-started:
			startedTopics[topic] = true
		case <-time.After(time.Second):
			t.Fatal("not all KKAI outbox runtime lanes started")
		}
	}
	require.Equal(t, map[string]bool{
		model.KKAIOutboxTopicTaskBillingAudit: true,
		KKAIOutboxTopicRiskActionCommitted:    true,
		model.KKAIOutboxTopicTopUpCompleted:   true,
	}, startedTopics)

	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	for index := range events {
		require.NoError(t, db.First(&events[index], events[index].ID).Error)
		require.Equal(t, model.KKAIOutboxStatusPending, events[index].Status)
		require.Equal(t, 1, events[index].Attempts)
		require.Empty(t, events[index].LockedBy)
		require.Equal(t, now.Add(5*time.Second).Unix(), events[index].AvailableAt)
	}
}

func TestKKAIOutboxRuntimeWorkerUsesGlobalBatchBudgetAcrossLanes(t *testing.T) {
	balancedTopics := make([]string, 0, 51)
	for range 17 {
		balancedTopics = append(balancedTopics,
			model.KKAIOutboxTopicTaskBillingAudit,
			KKAIOutboxTopicRiskActionCommitted,
			model.KKAIOutboxTopicTopUpCompleted,
		)
	}
	billingTopics := make([]string, 51)
	for index := range billingTopics {
		billingTopics[index] = model.KKAIOutboxTopicTaskBillingAudit
	}
	tests := []struct {
		name   string
		topics []string
	}{
		{
			name:   "caps combined lanes at fifty",
			topics: balancedTopics,
		},
		{
			name:   "idle lanes return capacity to billing",
			topics: billingTopics,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newOutboxTestDB(t)
			now := time.Unix(1_720_000_000, 0)
			for _, topic := range test.topics {
				seedOutboxEvent(t, db, topic, now.Unix())
			}
			noopHandler := func(context.Context, model.KKAIOutboxEvent) error { return nil }
			worker, err := newKKAIOutboxRuntimeWorker(db, "runtime-worker", kkaiOutboxRuntimeHandlers{
				taskBillingAudit:          noopHandler,
				taskBillingCacheReconcile: noopHandler,
				taskBillingRecovery:       noopHandler,
				taskAccounting:            noopHandler,
				riskActionCommitted:       noopHandler,
				topUpCompleted:            noopHandler,
			})
			require.NoError(t, err)
			for _, processor := range worker.processors {
				processor.now = func() time.Time { return now }
			}

			require.NoError(t, worker.ProcessOnce(context.Background()))
			var delivered int64
			require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).
				Where("status = ?", model.KKAIOutboxStatusDelivered).Count(&delivered).Error)
			require.EqualValues(t, defaultKKAIOutboxBatchLimit, delivered)
			var pending int64
			require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).
				Where("status = ?", model.KKAIOutboxStatusPending).Count(&pending).Error)
			require.EqualValues(t, len(test.topics)-defaultKKAIOutboxBatchLimit, pending)
		})
	}
}

func TestDecideKKAIRiskStreamEventRequiresConfirmedCausality(t *testing.T) {
	tests := []struct {
		name         string
		event        RiskStreamEvent
		wantDecision string
		wantActions  RiskDurableActions
		wantErr      error
	}{
		{
			name: "observe records without durable action",
			event: RiskStreamEvent{
				Recommendation: RiskDecisionObserve,
			},
		},
		{
			name: "confirmed client token disables token and user",
			event: RiskStreamEvent{
				Source:         RiskSourceEdgeGuard,
				Recommendation: RiskDecisionDisable,
				UserID:         10,
				TokenID:        11,
				Metadata: map[string]any{
					"evidence_level":              "confirmed",
					"causality":                   "client_token",
					"client_token_action_allowed": true,
				},
			},
			wantActions: RiskDurableActions{DisableToken: true, DisableUser: true},
		},
		{
			name: "confirmed upstream key is recorded without implicit channel disable",
			event: RiskStreamEvent{
				Source:         RiskSourceUpstreamPolicy,
				Recommendation: RiskDecisionDisable,
				ChannelID:      12,
				Metadata: map[string]any{
					"evidence_level": "confirmed",
					"causality":      "upstream_key",
				},
			},
			wantDecision: RiskDecisionReject,
		},
		{
			name: "unconfirmed evidence is rejected",
			event: RiskStreamEvent{
				Source:         RiskSourceEdgeGuard,
				Recommendation: RiskDecisionDisable,
				Metadata: map[string]any{
					"evidence_level": "suspected",
					"causality":      "client_token",
				},
			},
			wantErr: ErrRiskStreamDecisionRejected,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision, actions, err := DecideKKAIRiskStreamEvent(test.event)
			if test.wantErr != nil {
				require.ErrorIs(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			wantDecision := test.wantDecision
			if wantDecision == "" {
				wantDecision = test.event.Recommendation
			}
			require.Equal(t, wantDecision, decision)
			require.Equal(t, test.wantActions, actions)
		})
	}
}

func TestRiskActionOutboxHandlerDeliversCacheInvalidationAndNotification(t *testing.T) {
	var invalidatedUser int
	var invalidatedTokens int
	var refreshedChannels int
	var notified int
	handler := RiskActionOutboxHandler{
		InvalidateUser: func(userID int) error {
			invalidatedUser = userID
			return nil
		},
		InvalidateUserTokens: func(userID int) error {
			invalidatedTokens = userID
			return nil
		},
		RefreshChannels: func() { refreshedChannels++ },
		LookupIncident: func(_ context.Context, incidentID int64, eventID string) (model.KKAIPolicyIncident, error) {
			return model.KKAIPolicyIncident{
				ID: incidentID, EventID: eventID, Source: RiskSourceManualReview,
				UserID:        10,
				TokenDisabled: true, UserDisabled: true, ChannelDisabled: true,
			}, nil
		},
		Notify: func(riskActionOutboxPayload) error {
			notified++
			return nil
		},
	}
	payload, err := common.Marshal(riskActionOutboxPayload{
		IncidentID:      1,
		EventID:         "event-1",
		UserID:          10,
		TokenDisabled:   true,
		UserDisabled:    true,
		ChannelDisabled: true,
	})
	require.NoError(t, err)

	err = handler.Handle(context.Background(), model.KKAIOutboxEvent{Payload: string(payload)})
	require.NoError(t, err)
	require.Equal(t, 10, invalidatedUser)
	require.Equal(t, 10, invalidatedTokens)
	require.Equal(t, 1, refreshedChannels)
	require.Equal(t, 1, notified)
}

func TestRiskActionOutboxHandlerAuditOnlyDoesNotInvalidateAnything(t *testing.T) {
	invalidatedUser := 0
	invalidatedTokens := 0
	refreshedChannels := 0
	notified := 0
	handler := RiskActionOutboxHandler{
		InvalidateUser: func(userID int) error {
			invalidatedUser = userID
			return nil
		},
		InvalidateUserTokens: func(userID int) error {
			invalidatedTokens = userID
			return nil
		},
		RefreshChannels: func() { refreshedChannels++ },
		Notify: func(riskActionOutboxPayload) error {
			notified++
			return nil
		},
	}
	payload, err := common.Marshal(riskActionOutboxPayload{
		IncidentID: 1,
		EventID:    "event-audit-only",
		UserID:     10,
	})
	require.NoError(t, err)

	require.NoError(t, handler.Handle(context.Background(), model.KKAIOutboxEvent{Payload: string(payload)}))
	require.Equal(t, 0, invalidatedUser)
	require.Equal(t, 0, invalidatedTokens)
	require.Equal(t, 0, refreshedChannels)
	require.Equal(t, 1, notified)
}

func TestRiskActionOutboxHandlerRetriesFailedDelivery(t *testing.T) {
	expected := errors.New("redis unavailable")
	handler := RiskActionOutboxHandler{
		InvalidateUser:       func(int) error { return expected },
		InvalidateUserTokens: func(int) error { return nil },
		RefreshChannels:      func() {},
		LookupIncident: func(_ context.Context, incidentID int64, eventID string) (model.KKAIPolicyIncident, error) {
			return model.KKAIPolicyIncident{ID: incidentID, EventID: eventID, Source: RiskSourceManualReview, UserID: 10, UserDisabled: true}, nil
		},
		Notify: func(riskActionOutboxPayload) error { return nil },
	}
	payload, err := common.Marshal(riskActionOutboxPayload{EventID: "event-1", UserID: 10, UserDisabled: true})
	require.NoError(t, err)

	err = handler.Handle(context.Background(), model.KKAIOutboxEvent{Payload: string(payload)})
	require.ErrorIs(t, err, expected)
}

func TestRiskActionOutboxHandlerFailsClosedWhenIncidentLookupFails(t *testing.T) {
	expected := errors.New("incident store unavailable")
	invalidated := false
	notified := false
	handler := RiskActionOutboxHandler{
		InvalidateUser:       func(int) error { invalidated = true; return nil },
		InvalidateUserTokens: func(int) error { invalidated = true; return nil },
		RefreshChannels:      func() { invalidated = true },
		LookupIncident: func(context.Context, int64, string) (model.KKAIPolicyIncident, error) {
			return model.KKAIPolicyIncident{}, expected
		},
		Notify: func(riskActionOutboxPayload) error { notified = true; return nil },
	}
	payload, err := common.Marshal(riskActionOutboxPayload{
		IncidentID:      3,
		EventID:         "upstream-event-2",
		UserID:          10,
		TokenDisabled:   true,
		UserDisabled:    true,
		ChannelDisabled: true,
	})
	require.NoError(t, err)

	err = handler.Handle(context.Background(), model.KKAIOutboxEvent{Payload: string(payload)})
	require.ErrorIs(t, err, expected)
	require.False(t, invalidated)
	require.False(t, notified)
}

func TestRiskActionOutboxHandlerRejectsPayloadActionMismatch(t *testing.T) {
	invalidated := false
	notified := false
	handler := RiskActionOutboxHandler{
		InvalidateUser:       func(int) error { invalidated = true; return nil },
		InvalidateUserTokens: func(int) error { invalidated = true; return nil },
		RefreshChannels:      func() { invalidated = true },
		LookupIncident: func(_ context.Context, incidentID int64, eventID string) (model.KKAIPolicyIncident, error) {
			return model.KKAIPolicyIncident{ID: incidentID, EventID: eventID, Source: RiskSourceManualReview}, nil
		},
		Notify: func(riskActionOutboxPayload) error { notified = true; return nil },
	}
	payload, err := common.Marshal(riskActionOutboxPayload{
		IncidentID:    4,
		EventID:       "manual-event-1",
		UserID:        10,
		UserDisabled:  true,
		TokenDisabled: true,
	})
	require.NoError(t, err)

	err = handler.Handle(context.Background(), model.KKAIOutboxEvent{Payload: string(payload)})
	require.ErrorIs(t, err, ErrRiskActionInvalidInput)
	require.False(t, invalidated)
	require.False(t, notified)
}
