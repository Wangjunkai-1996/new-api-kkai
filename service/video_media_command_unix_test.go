//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExecVideoMediaCommandRunnerCancelsHungProcessGroup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()

	_, err := (execVideoMediaCommandRunner{}).Run(ctx, "/bin/sh", "-c", "sleep 30 & wait")

	require.Error(t, err)
	require.Less(t, time.Since(started), 2*time.Second)
}
