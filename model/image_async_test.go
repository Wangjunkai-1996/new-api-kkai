package model

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestImageBillingReservationParticipatesInCallerTransaction(t *testing.T) {
	db := newImageBillingTestDB(t)
	require.NoError(t, db.Create(&User{Id: 81, Username: "image-atomic", Password: "password", Quota: 1000}).Error)
	generation := seedImageBillingGeneration(t, db, 81)
	abort := errors.New("acceptance failed after reservation")
	err := db.Transaction(func(tx *gorm.DB) error {
		mutation, err := ReserveImageGenerationBillingTx(tx, generation.ID, TaskBillingSourceWallet, 300)
		require.NoError(t, err)
		assert.True(t, mutation.Applied)
		return abort
	})
	require.ErrorIs(t, err, abort)
	var user User
	require.NoError(t, db.First(&user, 81).Error)
	assert.EqualValues(t, 1000, user.Quota)
	require.NoError(t, db.First(&generation, generation.ID).Error)
	assert.Equal(t, ImageGenerationBillingStatePending, generation.BillingState)
	var events int64
	require.NoError(t, db.Model(&KKAIOutboxEvent{}).Count(&events).Error)
	assert.Zero(t, events)
}

func TestImageBillingSettlementAndRefundParticipateInCallerTransaction(t *testing.T) {
	for _, settle := range []bool{false, true} {
		t.Run(map[bool]string{false: "refund", true: "settle"}[settle], func(t *testing.T) {
			db := newImageBillingTestDB(t)
			require.NoError(t, db.Create(&User{Id: 82, Username: "image-finalize", Password: "password", Quota: 1000}).Error)
			generation := seedImageBillingGeneration(t, db, 82)
			_, err := ReserveImageGenerationBilling(context.Background(), db, generation.ID, TaskBillingSourceWallet, 300)
			require.NoError(t, err)
			if settle {
				prepareImageBillingAccounting(t, db, generation, 200, false)
			}
			abort := errors.New("fenced finalization aborted")
			err = db.Transaction(func(tx *gorm.DB) error {
				var err error
				if settle {
					_, err = SettleImageGenerationBillingTx(tx, generation.ID, 200, ImageGenerationStatusSubmitting)
				} else {
					_, err = RefundImageGenerationBillingTx(tx, generation.ID, ImageGenerationStatusSubmitting)
				}
				require.NoError(t, err)
				return abort
			})
			require.ErrorIs(t, err, abort)
			var user User
			require.NoError(t, db.First(&user, 82).Error)
			assert.EqualValues(t, 700, user.Quota)
			require.NoError(t, db.First(&generation, generation.ID).Error)
			assert.Equal(t, ImageGenerationBillingStateReserved, generation.BillingState)
			assert.Equal(t, 300, generation.ReservedQuota)
		})
	}
}

func TestImageWorkerFenceRejectsLostEventOrLeadership(t *testing.T) {
	db := newImageBillingTestDB(t)
	require.NoError(t, db.AutoMigrate(&KKAIJobLease{}, &KKAIImageExecution{}))
	now := time.Now().Unix()
	leader := KKAIJobLease{LeaseName: "background", Holder: "writer", Fence: 3, LeaseUntil: now + 300, UpdatedAt: now}
	require.NoError(t, db.Create(&leader).Error)
	event := KKAIOutboxEvent{EventKey: "image-generate-1", Topic: "image.generate.v1", AggregateID: "1", Payload: "{}", Status: KKAIOutboxStatusPending, LockedBy: "claim-a", LockedAt: now, CreatedAt: now}
	require.NoError(t, db.Create(&event).Error)
	fence := ImageExecutionFence{EventID: event.ID, LockedBy: event.LockedBy, LockTimeout: 2 * time.Minute, LeaderName: leader.LeaseName, LeaderHolder: leader.Holder, LeaderFence: leader.Fence}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return RequireImageWorkerFenceTx(tx, fence) }))
	leaderOnly := fence
	leaderOnly.EventID = 0
	leaderOnly.LockedBy = ""
	leaderOnly.LockTimeout = 0
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return RequireImageWorkerFenceTx(tx, leaderOnly) }))
	for _, change := range []struct {
		name   string
		mutate func(ImageExecutionFence) ImageExecutionFence
	}{
		{"old claim", func(f ImageExecutionFence) ImageExecutionFence { f.LockedBy = "claim-old"; return f }},
		{"old leader", func(f ImageExecutionFence) ImageExecutionFence { f.LeaderFence--; return f }},
		{"missing leader", func(f ImageExecutionFence) ImageExecutionFence { f.LeaderHolder = ""; return f }},
	} {
		t.Run(change.name, func(t *testing.T) {
			err := db.Transaction(func(tx *gorm.DB) error { return RequireImageWorkerFenceTx(tx, change.mutate(fence)) })
			require.ErrorIs(t, err, ErrImageExecutionFenceLost)
		})
	}
	require.NoError(t, db.Model(&KKAIOutboxEvent{}).Where("id = ?", event.ID).Update("locked_at", now-300).Error)
	require.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error { return RequireImageWorkerFenceTx(tx, fence) }), ErrImageExecutionFenceLost)
	require.NoError(t, db.Model(&KKAIJobLease{}).Where("lease_name = ?", leader.LeaseName).Update("lease_until", now-1).Error)
	require.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error { return RequireImageWorkerFenceTx(tx, leaderOnly) }), ErrImageExecutionFenceLost)
}

func TestImageCapacityIncludesLegacyAndReleasesOnlyTerminalTasks(t *testing.T) {
	db := newImageBillingTestDB(t)
	require.NoError(t, db.AutoMigrate(&KKAIJobLease{}))
	generation := seedImageBillingGeneration(t, db, 91)
	require.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error { return CheckImageGenerationCapacityTx(tx, 91, 2, 1) }), ErrImageGenerationCapacityExceeded)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return CheckImageGenerationCapacityTx(tx, 92, 2, 1) }))
	require.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error { return CheckImageGenerationCapacityTx(tx, 92, 1, 1) }), ErrImageGenerationCapacityExceeded)
	require.NoError(t, db.Model(&KKAIImageGeneration{}).Where("id = ?", generation.ID).Update("status", ImageGenerationStatusRecovering).Error)
	require.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error { return CheckImageGenerationCapacityTx(tx, 91, 2, 1) }), ErrImageGenerationCapacityExceeded)
	require.NoError(t, db.Model(&KKAIImageGeneration{}).Where("id = ?", generation.ID).Update("status", ImageGenerationStatusFailed).Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return CheckImageGenerationCapacityTx(tx, 91, 2, 1) }))
}

func TestImageExecutionCapacityIsSharedAcrossLegacyAndAsync(t *testing.T) {
	db := newImageBillingTestDB(t)
	require.NoError(t, db.AutoMigrate(&KKAIJobLease{}))
	legacy := seedImageBillingGeneration(t, db, 94)
	check := func(limit int, exclude int64) error {
		return db.Transaction(func(tx *gorm.DB) error { return CheckImageExecutionCapacityTx(tx, limit, exclude) })
	}
	// The bridge runtime protects legacy work before the v10 sidecars exist.
	require.ErrorIs(t, check(1, 0), ErrImageGenerationCapacityExceeded)
	require.NoError(t, db.AutoMigrate(&KKAIImageExecution{}))
	queued := seedImageBillingGeneration(t, db, 95)
	execution := KKAIImageExecution{GenerationID: queued.ID, State: ImageGenerationExecutionQueued, Payload: "{}", RecoveryDeadlineAt: time.Now().Add(time.Hour).Unix()}
	require.NoError(t, db.Create(&execution).Error)
	require.NoError(t, check(2, queued.ID))
	require.ErrorIs(t, check(1, queued.ID), ErrImageGenerationCapacityExceeded)
	require.NoError(t, db.Model(&execution).Update("state", ImageGenerationExecutionDispatching).Error)
	require.ErrorIs(t, check(2, 0), ErrImageGenerationCapacityExceeded)
	require.NoError(t, db.Model(&legacy).Update("status", ImageGenerationStatusSucceeded).Error)
	require.ErrorIs(t, check(1, 0), ErrImageGenerationCapacityExceeded)
	require.NoError(t, db.Model(&execution).Update("state", ImageGenerationExecutionUnknown).Error)
	require.ErrorIs(t, check(1, 0), ErrImageGenerationCapacityExceeded)
	require.NoError(t, db.Model(&execution).Update("recovery_deadline_at", time.Now().Add(-time.Second).Unix()).Error)
	require.NoError(t, check(1, 0))
}
