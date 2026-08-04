package model

import (
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("failed to open test db: " + err.Error())
	}
	DB = db
	LOG_DB = db

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true
	initCol()

	sqlDB, err := db.DB()
	if err != nil {
		panic("failed to get sql.DB: " + err.Error())
	}
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(
		&Task{},
		&User{},
		&UserSession{},
		&AuthFlow{},
		&ExternalIdentityClaim{},
		&Token{},
		&PasskeyCredential{},
		&TwoFA{},
		&TwoFABackupCode{},
		&Log{},
		&Channel{},
		&QuotaData{},
		&Ability{},
		&TopUp{},
		&SubscriptionPlan{},
		&SubscriptionOrder{},
		&UserSubscription{},
		&UserOAuthBinding{},
		&PerfMetric{},
		&SystemInstance{},
		&SystemTask{},
		&SystemTaskLock{},
		&KKAIOutboxEvent{},
	); err != nil {
		panic("failed to migrate: " + err.Error())
	}

	os.Exit(m.Run())
}

func truncateTables(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		DB.Exec("DELETE FROM tasks")
		DB.Exec("DELETE FROM auth_flows")
		DB.Exec("DELETE FROM external_identity_claims")
		DB.Exec("DELETE FROM user_sessions")
		DB.Exec("DELETE FROM passkey_credentials")
		DB.Exec("DELETE FROM two_fa_backup_codes")
		DB.Exec("DELETE FROM two_fas")
		DB.Exec("DELETE FROM tokens")
		DB.Exec("DELETE FROM user_oauth_bindings")
		DB.Exec("DELETE FROM users")
		DB.Exec("DELETE FROM logs")
		DB.Exec("DELETE FROM channels")
		DB.Exec("DELETE FROM quota_data")
		DB.Exec("DELETE FROM abilities")
		DB.Exec("DELETE FROM top_ups")
		DB.Exec("DELETE FROM subscription_orders")
		DB.Exec("DELETE FROM subscription_plans")
		DB.Exec("DELETE FROM user_subscriptions")
		DB.Exec("DELETE FROM perf_metrics")
		DB.Exec("DELETE FROM system_instances")
		DB.Exec("DELETE FROM system_task_locks")
		DB.Exec("DELETE FROM system_tasks")
		DB.Exec("DELETE FROM kkai_outbox")
	})
}

func insertTask(t *testing.T, task *Task) {
	t.Helper()
	task.CreatedAt = time.Now().Unix()
	task.UpdatedAt = time.Now().Unix()
	require.NoError(t, DB.Create(task).Error)
}

// ---------------------------------------------------------------------------
// Snapshot / Equal — pure logic tests (no DB)
// ---------------------------------------------------------------------------

func TestSnapshotEqual_Same(t *testing.T) {
	s := taskSnapshot{
		Status:     TaskStatusInProgress,
		Progress:   "50%",
		StartTime:  1000,
		FinishTime: 0,
		FailReason: "",
		ResultURL:  "",
		Data:       json.RawMessage(`{"key":"value"}`),
	}
	assert.True(t, s.Equal(s))
}

func TestSnapshotEqual_DifferentStatus(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage(`{}`)}
	b := taskSnapshot{Status: TaskStatusSuccess, Data: json.RawMessage(`{}`)}
	assert.False(t, a.Equal(b))
}

func TestSnapshotEqual_DifferentProgress(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Progress: "30%", Data: json.RawMessage(`{}`)}
	b := taskSnapshot{Status: TaskStatusInProgress, Progress: "60%", Data: json.RawMessage(`{}`)}
	assert.False(t, a.Equal(b))
}

func TestSnapshotEqual_DifferentData(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage(`{"a":1}`)}
	b := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage(`{"a":2}`)}
	assert.False(t, a.Equal(b))
}

func TestSnapshotEqual_NilVsEmpty(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Data: nil}
	b := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage{}}
	// bytes.Equal(nil, []byte{}) == true
	assert.True(t, a.Equal(b))
}

func TestSnapshot_Roundtrip(t *testing.T) {
	task := &Task{
		Status:     TaskStatusInProgress,
		Progress:   "42%",
		StartTime:  1234,
		FinishTime: 5678,
		FailReason: "timeout",
		PrivateData: TaskPrivateData{
			ResultURL: "https://example.com/result.mp4",
		},
		Data: json.RawMessage(`{"model":"test-model"}`),
	}
	snap := task.Snapshot()
	assert.Equal(t, task.Status, snap.Status)
	assert.Equal(t, task.Progress, snap.Progress)
	assert.Equal(t, task.StartTime, snap.StartTime)
	assert.Equal(t, task.FinishTime, snap.FinishTime)
	assert.Equal(t, task.FailReason, snap.FailReason)
	assert.Equal(t, task.PrivateData.ResultURL, snap.ResultURL)
	assert.JSONEq(t, string(task.Data), string(snap.Data))
}

// ---------------------------------------------------------------------------
// UpdateWithStatus CAS — DB integration tests
// ---------------------------------------------------------------------------

func TestUpdateWithStatus_Win(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID:   "task_cas_win",
		Status:   TaskStatusInProgress,
		Progress: "50%",
		Data:     json.RawMessage(`{}`),
	}
	insertTask(t, task)

	task.Status = TaskStatusSuccess
	task.Progress = "100%"
	won, err := task.UpdateWithStatus(TaskStatusInProgress)
	require.NoError(t, err)
	assert.True(t, won)

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, TaskStatusSuccess, reloaded.Status)
	assert.Equal(t, "100%", reloaded.Progress)
}

func TestUpdateWithStatus_Lose(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID: "task_cas_lose",
		Status: TaskStatusFailure,
		Data:   json.RawMessage(`{}`),
	}
	insertTask(t, task)

	task.Status = TaskStatusSuccess
	won, err := task.UpdateWithStatus(TaskStatusInProgress) // wrong fromStatus
	require.NoError(t, err)
	assert.False(t, won)

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, TaskStatusFailure, reloaded.Status) // unchanged
}

func TestUpdateWithStatus_ConcurrentWinner(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID: "task_cas_race",
		Status: TaskStatusInProgress,
		Quota:  1000,
		Data:   json.RawMessage(`{}`),
	}
	insertTask(t, task)

	const goroutines = 5
	wins := make([]bool, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			t := &Task{}
			*t = Task{
				ID:       task.ID,
				TaskID:   task.TaskID,
				Status:   TaskStatusSuccess,
				Progress: "100%",
				Quota:    task.Quota,
				Data:     json.RawMessage(`{}`),
			}
			t.CreatedAt = task.CreatedAt
			t.UpdatedAt = time.Now().Unix()
			won, err := t.UpdateWithStatus(TaskStatusInProgress)
			if err == nil {
				wins[idx] = won
			}
		}(i)
	}
	wg.Wait()

	winCount := 0
	for _, w := range wins {
		if w {
			winCount++
		}
	}
	assert.Equal(t, 1, winCount, "exactly one goroutine should win the CAS")
}

func TestUpdateWithStatusPreservingBillingDoesNotOverwriteConcurrentSettlement(t *testing.T) {
	truncateTables(t)

	targetQuota := 150
	task := &Task{
		TaskID:   "task_billing_cas",
		Status:   TaskStatusSubmitted,
		Progress: "10%",
		Quota:    100,
		PrivateData: TaskPrivateData{
			ResultURL:    "",
			BillingState: TaskBillingStateAccepted,
			TokenQuota:   100,
			TokenBilling: true,
			TargetQuota:  &targetQuota,
			BillingContext: &TaskBillingContext{
				OriginModelName: "frozen-model",
				MaxQuota:        &targetQuota,
			},
		},
		Data: json.RawMessage(`{"id":"upstream"}`),
	}
	insertTask(t, task)
	stalePollerTask := *task
	staleSnapshot := stalePollerTask.Snapshot()

	settledTask := *task
	settledTask.Quota = targetQuota
	settledTask.PrivateData.TokenQuota = targetQuota
	settledTask.PrivateData.TargetQuota = nil
	require.NoError(t, DB.Save(&settledTask).Error)

	stalePollerTask.Status = TaskStatusSuccess
	stalePollerTask.Progress = "100%"
	stalePollerTask.PrivateData.ResultURL = "https://example.com/result.mp4"
	stalePollerTask.PrivateData.ArchiveSource = "data:video/mp4;base64,dmlkZW8="
	won, err := stalePollerTask.UpdateWithStatusPreservingBilling(staleSnapshot)
	require.NoError(t, err)
	require.True(t, won)

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, TaskStatusSuccess, reloaded.Status)
	assert.Equal(t, "100%", reloaded.Progress)
	assert.Equal(t, targetQuota, reloaded.Quota)
	assert.Equal(t, targetQuota, reloaded.PrivateData.TokenQuota)
	assert.Nil(t, reloaded.PrivateData.TargetQuota)
	assert.Equal(t, TaskBillingStateAccepted, reloaded.PrivateData.BillingState)
	assert.Equal(t, "https://example.com/result.mp4", reloaded.PrivateData.ResultURL)
	assert.Equal(t, "data:video/mp4;base64,dmlkZW8=", reloaded.PrivateData.ArchiveSource)
}

func TestUpdateWithStatusPreservingBillingRejectsStaleSameStatusPoller(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID:   "task_same_status_stale",
		Status:   TaskStatusInProgress,
		Progress: "10%",
		Data:     json.RawMessage(`{"frame":"initial"}`),
	}
	insertTask(t, task)
	initialSnapshot := task.Snapshot()
	newerPoller := *task
	stalePoller := *task

	newerPoller.Progress = "60%"
	newerPoller.PrivateData.ResultURL = "https://example.com/new.mp4"
	newerPoller.Data = json.RawMessage(`{"frame":"new"}`)
	won, err := newerPoller.UpdateWithStatusPreservingBilling(initialSnapshot)
	require.NoError(t, err)
	require.True(t, won)

	stalePoller.Progress = "30%"
	stalePoller.PrivateData.ResultURL = "https://example.com/stale.mp4"
	stalePoller.Data = json.RawMessage(`{"frame":"stale"}`)
	won, err = stalePoller.UpdateWithStatusPreservingBilling(initialSnapshot)
	require.NoError(t, err)
	assert.False(t, won)

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	assert.Equal(t, "60%", reloaded.Progress)
	assert.Equal(t, "https://example.com/new.mp4", reloaded.PrivateData.ResultURL)
	assert.JSONEq(t, `{"frame":"new"}`, string(reloaded.Data))

	currentSnapshot := reloaded.Snapshot()
	reloaded.Progress = "40%"
	won, err = reloaded.UpdateWithStatusPreservingBilling(currentSnapshot)
	require.NoError(t, err)
	assert.False(t, won, "a fresh same-status poll must not move percentage backwards")
}

func TestFailTaskBeforeSubmissionRefusesLiveDispatcher(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID:   "task_live_dispatcher",
		Status:   TaskStatusNotStart,
		Progress: "0%",
		Quota:    100,
		PrivateData: TaskPrivateData{
			BillingState: TaskBillingStateDispatching,
			TokenQuota:   100,
			TokenBilling: true,
		},
		Data: json.RawMessage(`{}`),
	}
	insertTask(t, task)

	updated, failed, err := FailTaskBeforeSubmission(nil, task.ID, "task submission failed")
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.False(t, failed)
	assert.Equal(t, TaskBillingStateDispatching, updated.PrivateData.BillingState)
	assert.EqualValues(t, TaskStatusNotStart, updated.Status)

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	assert.Equal(t, TaskBillingStateDispatching, reloaded.PrivateData.BillingState)
	assert.EqualValues(t, TaskStatusNotStart, reloaded.Status)
}

func TestMarkTaskSubmissionAmbiguousPreservesBillingSnapshot(t *testing.T) {
	truncateTables(t)

	targetQuota := 120
	maxQuota := 150
	task := &Task{
		TaskID:   "task_ambiguous_submission",
		Status:   TaskStatusNotStart,
		Progress: "0%",
		Quota:    100,
		PrivateData: TaskPrivateData{
			BillingState: TaskBillingStateDispatching,
			TokenQuota:   100,
			TokenBilling: true,
			BillingContext: &TaskBillingContext{
				OriginModelName: "frozen-model",
				MaxQuota:        &maxQuota,
				OtherRatios:     map[string]float64{"duration": 2},
			},
		},
		Data: json.RawMessage(`{"request":"snapshot"}`),
	}
	insertTask(t, task)

	markedTask, marked, err := MarkTaskSubmissionAmbiguous(nil, task.ID, TaskSubmissionAmbiguity{
		Reason:      "upstream response was lost",
		TargetQuota: &targetQuota,
	})
	require.NoError(t, err)
	require.True(t, marked)
	require.NotNil(t, markedTask)
	assert.EqualValues(t, TaskStatusUnknown, markedTask.Status)
	assert.Equal(t, "100%", markedTask.Progress)
	assert.Equal(t, "upstream response was lost", markedTask.FailReason)
	assert.NotZero(t, markedTask.FinishTime)
	assert.Equal(t, TaskBillingStateAmbiguous, markedTask.PrivateData.BillingState)
	assert.True(t, markedTask.PrivateData.AccountingRequired)
	assert.Equal(t, TaskAccountingStatePending, markedTask.PrivateData.AccountingState)
	require.NotNil(t, markedTask.PrivateData.TargetQuota)
	assert.Equal(t, targetQuota, *markedTask.PrivateData.TargetQuota)
	assert.Equal(t, 100, markedTask.Quota)
	assert.Equal(t, 100, markedTask.PrivateData.TokenQuota)
	assert.True(t, markedTask.PrivateData.TokenBilling)
	require.NotNil(t, markedTask.PrivateData.BillingContext)
	assert.Equal(t, maxQuota, *markedTask.PrivateData.BillingContext.MaxQuota)
	assert.Equal(t, 2.0, markedTask.PrivateData.BillingContext.OtherRatios["duration"])
	assert.JSONEq(t, `{"request":"snapshot"}`, string(markedTask.Data))

	firstFinishTime := markedTask.FinishTime
	markedTask, marked, err = MarkTaskSubmissionAmbiguous(nil, task.ID, TaskSubmissionAmbiguity{
		Reason: "retry must not replace the original reason",
	})
	require.NoError(t, err)
	require.True(t, marked)
	assert.Equal(t, firstFinishTime, markedTask.FinishTime)
	assert.Equal(t, "upstream response was lost", markedTask.FailReason)
	require.NotNil(t, markedTask.PrivateData.TargetQuota)
	assert.Equal(t, targetQuota, *markedTask.PrivateData.TargetQuota)
}

func TestMarkTaskSubmissionAmbiguousRefusesAcceptedTask(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID:   "task_already_accepted",
		Status:   TaskStatusSubmitted,
		Progress: "10%",
		Quota:    100,
		PrivateData: TaskPrivateData{
			BillingState:       TaskBillingStateAccepted,
			UpstreamTaskID:     "upstream-task",
			AccountingRequired: true,
			AccountingState:    TaskAccountingStatePending,
		},
		Data: json.RawMessage(`{"id":"upstream-task"}`),
	}
	insertTask(t, task)

	markedTask, marked, err := MarkTaskSubmissionAmbiguous(nil, task.ID, TaskSubmissionAmbiguity{
		Reason: "stale dispatcher",
	})
	require.NoError(t, err)
	require.NotNil(t, markedTask)
	assert.False(t, marked)
	assert.Equal(t, TaskBillingStateAccepted, markedTask.PrivateData.BillingState)
	assert.EqualValues(t, TaskStatusSubmitted, markedTask.Status)
	assert.Equal(t, "upstream-task", markedTask.PrivateData.UpstreamTaskID)
}

func TestPersistTaskSubmissionAcceptanceRefusesRecoveredTask(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID:   "task_recovered_before_acceptance",
		Status:   TaskStatusUnknown,
		Progress: "100%",
		Quota:    100,
		PrivateData: TaskPrivateData{
			BillingState: TaskBillingStateCompleted,
			TokenQuota:   100,
			TokenBilling: true,
			BillingContext: &TaskBillingContext{
				OriginModelName: "frozen-model",
			},
		},
		Data: json.RawMessage(`{}`),
	}
	insertTask(t, task)

	updated, accepted, err := PersistTaskSubmissionAcceptance(nil, task.ID, TaskSubmissionAcceptance{
		UpstreamTaskID: "late-upstream-id",
		TaskData:       json.RawMessage(`{"id":"late-upstream-id"}`),
		Status:         TaskStatusSubmitted,
		Progress:       "10%",
		TargetQuota:    100,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.False(t, accepted)
	assert.Equal(t, TaskBillingStateCompleted, updated.PrivateData.BillingState)
	assert.Empty(t, updated.PrivateData.UpstreamTaskID)
	assert.EqualValues(t, TaskStatusUnknown, updated.Status)

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	assert.Equal(t, TaskBillingStateCompleted, reloaded.PrivateData.BillingState)
	assert.Empty(t, reloaded.PrivateData.UpstreamTaskID)
	assert.EqualValues(t, TaskStatusUnknown, reloaded.Status)
}
