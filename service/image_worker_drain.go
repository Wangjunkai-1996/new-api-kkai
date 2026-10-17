package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/google/uuid"
)

const (
	ImageWorkerStatusPath      = "/tmp/newapi-image-worker-status.json"
	ImageWorkerLeaderLeaseName = "newapi-background-writer"
)

var ErrImageWorkerDrainUnavailable = errors.New("image worker drain status unavailable")

type ImageWorkerDrainStatus struct {
	SchemaVersion int    `json:"schema_version"`
	StartupID     string `json:"startup_id"`
	WorkerID      string `json:"worker_id"`
	PID           int    `json:"pid"`
	StartedAt     string `json:"started_at"`
	UpdatedAt     string `json:"updated_at"`
	Draining      bool   `json:"draining"`
	Inflight      int    `json:"inflight"`
}

type imageWorkerDrainGate struct {
	mu       sync.Mutex
	path     string
	status   ImageWorkerDrainStatus
	writeErr error
	changed  chan struct{}
}

var imageWorkerGate = &imageWorkerDrainGate{}

func InitializeImageWorkerDrain(workerID, path string) error {
	return imageWorkerGate.initialize(workerID, path)
}

func (gate *imageWorkerDrainGate) initialize(workerID, path string) error {
	if !leaderLeaseNamePattern.MatchString(workerID) || path == "" {
		return ErrImageWorkerDrainUnavailable
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.status.StartupID != "" {
		return ErrImageWorkerDrainUnavailable
	}
	gate.path = path
	gate.status = ImageWorkerDrainStatus{SchemaVersion: 1, StartupID: uuid.NewString(), WorkerID: workerID, PID: os.Getpid(), StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	gate.changed = make(chan struct{})
	return gate.publishLocked()
}

// BeginImageWorkerOperation must cover one claim-and-execute cycle, not an
// entire polling loop. Drain and admission use the same mutex, closing the race
// between observing zero in-flight operations and admitting another claim.
func BeginImageWorkerOperation() (func(), bool) { return imageWorkerGate.begin() }

func (gate *imageWorkerDrainGate) begin() (func(), bool) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.status.StartupID == "" || gate.status.Draining || gate.writeErr != nil {
		return nil, false
	}
	gate.status.Inflight++
	if err := gate.publishLocked(); err != nil {
		gate.status.Inflight--
		gate.status.Draining = true
		common.SysError("image worker admission stopped: " + err.Error())
		return nil, false
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			gate.mu.Lock()
			defer gate.mu.Unlock()
			gate.status.Inflight--
			if err := gate.publishLocked(); err != nil {
				gate.status.Draining = true
				common.SysError("image worker completion status failed: " + err.Error())
			}
		})
	}, true
}

func BeginImageWorkerDrain() error { return imageWorkerGate.setDraining(true) }
func ResumeImageWorker() error     { return imageWorkerGate.setDraining(false) }

func (gate *imageWorkerDrainGate) setDraining(draining bool) error {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.status.StartupID == "" {
		return ErrImageWorkerDrainUnavailable
	}
	gate.status.Draining = draining
	if err := gate.publishLocked(); err != nil {
		gate.status.Draining = true
		return err
	}
	return nil
}

func ImageWorkerStatus() (ImageWorkerDrainStatus, error) {
	imageWorkerGate.mu.Lock()
	defer imageWorkerGate.mu.Unlock()
	if imageWorkerGate.status.StartupID == "" {
		return ImageWorkerDrainStatus{}, ErrImageWorkerDrainUnavailable
	}
	return imageWorkerGate.status, imageWorkerGate.writeErr
}

func WaitForImageWorkerDrain(ctx context.Context) error { return imageWorkerGate.wait(ctx) }

func (gate *imageWorkerDrainGate) wait(ctx context.Context) error {
	for {
		gate.mu.Lock()
		if gate.status.StartupID == "" || !gate.status.Draining {
			gate.mu.Unlock()
			return ErrImageWorkerDrainUnavailable
		}
		if gate.writeErr != nil {
			err := gate.writeErr
			gate.mu.Unlock()
			return err
		}
		if gate.status.Inflight == 0 {
			gate.mu.Unlock()
			return nil
		}
		changed := gate.changed
		gate.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

func (gate *imageWorkerDrainGate) publishLocked() (writeErr error) {
	gate.status.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	defer func() {
		gate.writeErr = writeErr
		if gate.changed != nil {
			close(gate.changed)
			gate.changed = make(chan struct{})
		}
	}()
	encoded, err := common.Marshal(gate.status)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(gate.path), ".image-worker-status-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err := file.Write(encoded); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(name, gate.path)
}

func WithCurrentImageWorkerLease(ctx context.Context, workerID string) (context.Context, error) {
	lease, err := NewKKAILeaderLeaseStore(model.DB, ImageWorkerLeaderLeaseName).Current(ctx)
	if err != nil {
		return ctx, err
	}
	if lease.Holder != workerID || lease.Fence <= 0 || lease.LeaseUntil <= time.Now().Unix() {
		return ctx, fmt.Errorf("%w: image worker does not hold the leader lease", model.ErrImageExecutionFenceLost)
	}
	return WithImageWorkerLease(ctx, *lease), nil
}
