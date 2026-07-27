//go:build windows || aix || solaris || plan9

package service

import (
	"context"
	"os/exec"
	"time"
)

func (execVideoMediaCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.WaitDelay = 2 * time.Second
	output := &boundedVideoMediaCommandOutput{}
	command.Stdout = output
	command.Stderr = output
	err := command.Run()
	if output.overflow {
		return output.Bytes(), ErrVideoMediaProcessingFailed
	}
	return output.Bytes(), err
}
