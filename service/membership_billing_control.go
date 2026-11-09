package service

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const MembershipRecoveryControlProtocol = "membership-recovery-control-v1"

type MembershipRecoveryRuntimeStatus struct {
	Protocol       string `json:"protocol"`
	Version        string `json:"version"`
	StartTime      int64  `json:"start_time"`
	PID            int    `json:"pid"`
	WorkerID       string `json:"worker_id"`
	NodeRole       string `json:"node_role"`
	ConfiguredMode string `json:"configured_mode"`
	EffectiveMode  string `json:"effective_mode"`
	PersistedMode  string `json:"persisted_mode"`
	Generation     string `json:"generation"`
	Synchronized   bool   `json:"synchronized"`
	Inflight       bool   `json:"inflight"`
	ObservedAt     int64  `json:"observed_at"`
}

var membershipRecoveryRuntime = struct {
	refresh sync.Mutex
	sync.Mutex
	workerID   string
	configured string
	control    model.MembershipRecoveryControl
	ready      bool
	observedAt int64
	cancel     context.CancelFunc
	wakeup     chan struct{}
}{wakeup: make(chan struct{}, 1)}

func initializeMembershipRecoveryControl(workerID, configured string) {
	membershipRecoveryRuntime.Lock()
	defer membershipRecoveryRuntime.Unlock()
	if membershipRecoveryRuntime.cancel != nil {
		membershipRecoveryRuntime.cancel()
	}
	membershipRecoveryRuntime.workerID, membershipRecoveryRuntime.configured = workerID, configured
	membershipRecoveryRuntime.control = model.MembershipRecoveryControl{Mode: "paused"}
	membershipRecoveryRuntime.ready = false
	membershipBillingRecoveryMode.Store("paused")
}

// This bounded read job runs independently of the financial job, including on
// serving processes. It never runs on a request or first-content path.
func MembershipBillingRecoveryControlPollJob() BackgroundJob {
	return BackgroundJob{Name: "membership-recovery-control", Interval: time.Second, RunOnStart: true,
		Run: func(ctx context.Context) error {
			_, err := refreshMembershipRecoveryControl(ctx)
			return err
		}}
}

func refreshMembershipRecoveryControl(parent context.Context) (model.MembershipRecoveryControl, error) {
	membershipRecoveryRuntime.refresh.Lock()
	defer membershipRecoveryRuntime.refresh.Unlock()
	ctx, cancel := context.WithTimeout(parent, time.Second)
	defer cancel()
	control, err := model.ReadMembershipRecoveryControl(ctx)
	if err == nil && control.Mode != "paused" {
		err = CheckMembershipBillingRecoveryRuntimeSchema(ctx, model.DB)
	}
	membershipRecoveryRuntime.Lock()
	defer membershipRecoveryRuntime.Unlock()
	changed := control != membershipRecoveryRuntime.control || membershipRecoveryRuntime.ready != (err == nil)
	membershipRecoveryRuntime.control, membershipRecoveryRuntime.ready = control, err == nil
	membershipRecoveryRuntime.observedAt = time.Now().UnixMilli()
	mode := control.Mode
	if err != nil {
		mode = "paused"
	}
	membershipBillingRecoveryMode.Store(mode)
	if (changed || err != nil) && membershipRecoveryRuntime.cancel != nil {
		membershipRecoveryRuntime.cancel()
	}
	if changed && err == nil && mode != "paused" {
		select {
		case membershipRecoveryRuntime.wakeup <- struct{}{}:
		default:
		}
	}
	return control, err
}

func beginMembershipRecoveryRound(parent context.Context, control model.MembershipRecoveryControl) (context.Context, func(), error) {
	membershipRecoveryRuntime.Lock()
	defer membershipRecoveryRuntime.Unlock()
	if !membershipRecoveryRuntime.ready || control != membershipRecoveryRuntime.control || control.Mode == "paused" || membershipRecoveryRuntime.cancel != nil {
		return nil, nil, model.ErrMembershipRecoveryControl
	}
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	membershipRecoveryRuntime.cancel = cancel
	var once sync.Once
	return ctx, func() {
		once.Do(func() {
			cancel()
			membershipRecoveryRuntime.Lock()
			membershipRecoveryRuntime.cancel = nil
			membershipRecoveryRuntime.Unlock()
		})
	}, nil
}

// A pause is acknowledged only after the observed durable generation matches
// and Inflight is false. Cancellation and waiting never hold a User lock.
func MembershipBillingRecoveryRuntimeStatus(ctx context.Context) (MembershipRecoveryRuntimeStatus, error) {
	_, err := refreshMembershipRecoveryControl(ctx)
	membershipRecoveryRuntime.Lock()
	defer membershipRecoveryRuntime.Unlock()
	return MembershipRecoveryRuntimeStatus{
		Protocol: MembershipRecoveryControlProtocol, Version: common.Version, StartTime: common.StartTime,
		PID: os.Getpid(), WorkerID: membershipRecoveryRuntime.workerID, NodeRole: string(common.CurrentNodeRole()),
		ConfiguredMode: membershipRecoveryRuntime.configured, EffectiveMode: MembershipBillingRecoveryMode(),
		PersistedMode: membershipRecoveryRuntime.control.Mode, Generation: membershipRecoveryRuntime.control.Generation,
		Synchronized: membershipRecoveryRuntime.ready, Inflight: membershipRecoveryRuntime.cancel != nil,
		ObservedAt: membershipRecoveryRuntime.observedAt,
	}, err
}
