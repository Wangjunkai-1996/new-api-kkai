//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package service

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func (execVideoMediaCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = 2 * time.Second
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
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
