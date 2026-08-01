//go:build windows || aix || solaris || plan9

package service

import (
	"context"
	"errors"
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
	if ctxErr := ctx.Err(); ctxErr != nil {
		return output.Bytes(), ctxErr
	}
	if output.overflow {
		if err != nil {
			return output.Bytes(), errors.Join(ErrVideoMediaProcessingFailed, err)
		}
		return output.Bytes(), ErrVideoMediaProcessingFailed
	}
	return output.Bytes(), err
}
