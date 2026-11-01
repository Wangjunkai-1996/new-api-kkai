package common

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewReplayableBodyReaderKeepsStorageLifecycleWithCaller(t *testing.T) {
	payload := []byte(`{"model":"test-model","input":"hello"}`)
	storage, err := CreateBodyStorage(payload)
	require.NoError(t, err)
	defer storage.Close()

	body := NewReplayableBodyReader(storage)
	assert.EqualValues(t, len(payload), body.Size())
	_, exposesCloser := any(body).(io.Closer)
	assert.False(t, exposesCloser, "the request body must not expose the storage closer")

	req, err := http.NewRequest(http.MethodPost, "https://example.com", body)
	require.NoError(t, err)
	require.NoError(t, req.Body.Close())

	replayBody, err := body.NewReader()
	require.NoError(t, err, "closing the HTTP request body must not close the storage")
	replay, err := io.ReadAll(replayBody)
	require.NoError(t, err)
	require.NoError(t, replayBody.Close())
	assert.Equal(t, payload, replay)

	require.NoError(t, storage.Close())
	_, err = body.NewReader()
	require.ErrorIs(t, err, ErrStorageClosed)
}

func TestReadOnlyBodyBytesOnlyForMemoryReplay(t *testing.T) {
	payload := []byte(`{"input":"hello"}`)
	memory := newMemoryStorage(payload)
	t.Cleanup(func() { require.NoError(t, memory.Close()) })
	first, err := memory.NewReader()
	require.NoError(t, err)
	view, ok := ReadOnlyBodyBytes(first)
	require.True(t, ok)
	require.NotEmpty(t, view)
	assert.Equal(t, &payload[0], &view[0], "the view must share the stored backing")
	partial := make([]byte, 4)
	_, err = first.Read(partial)
	require.NoError(t, err)
	second, err := memory.NewReader()
	require.NoError(t, err)
	assert.Equal(t, []byte(`{"in`), partial)
	assert.Equal(t, payload, view)
	require.NoError(t, first.Close())
	actual, err := io.ReadAll(second)
	require.NoError(t, err)
	assert.Equal(t, payload, actual)
	require.NoError(t, second.Close())

	disk, err := newDiskStorage(payload, t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, disk.Close()) })
	diskReader, err := disk.NewReader()
	require.NoError(t, err)
	_, ok = ReadOnlyBodyBytes(diskReader)
	assert.False(t, ok)
	require.NoError(t, diskReader.Close())
}
