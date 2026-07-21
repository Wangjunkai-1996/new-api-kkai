package main

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/logger"
)

type recoveryOutputWriter struct {
	written int
	err     error
}

func (writer recoveryOutputWriter) Write(value []byte) (int, error) {
	if writer.written > len(value) {
		return len(value), writer.err
	}
	return writer.written, writer.err
}

func TestOpenDatabaseKeepsMachineOutputFreeOfGORMLogs(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "recovery.db")
	db, err := openDatabase(dsn)
	require.NoError(t, err)
	require.Same(t, logger.Discard, db.Config.Logger)
}

func TestWriteJSONWritesOneMachineReadableLine(t *testing.T) {
	var output bytes.Buffer
	err := writeJSON(&output, struct {
		Mode string `json:"mode"`
	}{Mode: "verify"})
	require.NoError(t, err)
	assert.Equal(t, "{\"mode\":\"verify\"}\n", output.String())
}

func TestWriteJSONReturnsOutputFailures(t *testing.T) {
	expected := errors.New("stdout closed")
	err := writeJSON(recoveryOutputWriter{err: expected}, map[string]string{"mode": "apply"})
	require.ErrorIs(t, err, expected)

	err = writeJSON(recoveryOutputWriter{written: 1}, map[string]string{"mode": "apply"})
	require.ErrorIs(t, err, io.ErrShortWrite)
}
