package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readImageWorkerTestStatus(t *testing.T, path string) ImageWorkerDrainStatus {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	var status ImageWorkerDrainStatus
	require.NoError(t, common.Unmarshal(content, &status))
	return status
}

func TestImageWorkerDrainStopsClaimsAndWaitsForExistingWork(t *testing.T) {
	gate := &imageWorkerDrainGate{}
	path := filepath.Join(t.TempDir(), "status.json")
	require.NoError(t, gate.initialize("writer-test", path))
	finish, admitted := gate.begin()
	require.True(t, admitted)
	require.NoError(t, gate.setDraining(true))
	status := readImageWorkerTestStatus(t, path)
	assert.True(t, status.Draining)
	assert.Equal(t, 1, status.Inflight)
	_, admitted = gate.begin()
	assert.False(t, admitted)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	assert.ErrorIs(t, gate.wait(canceled), context.Canceled)
	finish()
	finish()
	require.NoError(t, gate.wait(context.Background()))
	status = readImageWorkerTestStatus(t, path)
	assert.True(t, status.Draining)
	assert.Zero(t, status.Inflight)
	require.NoError(t, gate.setDraining(false))
	finish, admitted = gate.begin()
	require.True(t, admitted)
	finish()
}

func TestImageWorkerStatusWriteFailureFailsClosed(t *testing.T) {
	gate := &imageWorkerDrainGate{}
	path := filepath.Join(t.TempDir(), "status.json")
	require.NoError(t, gate.initialize("writer-test", path))
	finish, admitted := gate.begin()
	require.True(t, admitted)
	require.NoError(t, gate.setDraining(true))
	gate.path = filepath.Join(t.TempDir(), "missing", "status.json")
	finish()
	assert.Error(t, gate.wait(context.Background()))
	assert.Equal(t, 1, readImageWorkerTestStatus(t, path).Inflight, "a failed write must not publish a false zero")
	_, admitted = gate.begin()
	assert.False(t, admitted)
	assert.Error(t, gate.setDraining(false))
	_, admitted = gate.begin()
	assert.False(t, admitted)
	gate.path = path
	require.NoError(t, gate.setDraining(true))
	require.NoError(t, gate.wait(context.Background()))
}

func TestImageWorkerStartupReplacesStaleDrainIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	old := &imageWorkerDrainGate{}
	require.NoError(t, old.initialize("old-writer", path))
	require.NoError(t, old.setDraining(true))
	previous := readImageWorkerTestStatus(t, path)
	current := &imageWorkerDrainGate{}
	require.NoError(t, current.initialize("new-writer", path))
	status := readImageWorkerTestStatus(t, path)
	assert.Equal(t, 1, status.SchemaVersion)
	assert.Equal(t, "new-writer", status.WorkerID)
	assert.Equal(t, os.Getpid(), status.PID)
	assert.NotEqual(t, previous.StartupID, status.StartupID)
	assert.False(t, status.Draining)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
	uninitialized := &imageWorkerDrainGate{}
	_, admitted := uninitialized.begin()
	assert.False(t, admitted)
	assert.ErrorIs(t, uninitialized.setDraining(true), ErrImageWorkerDrainUnavailable)
}
