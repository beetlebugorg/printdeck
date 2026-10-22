//go:build linux || darwin

package awake

import (
	"os/exec"
)

// hold starts a program that keeps the computer awake until it stops: systemd-inhibit on Linux,
// caffeinate on macOS.
func hold(why string) (func(), error) {
	cmd := exec.Command(holdCommand[0], append(holdCommand[1:], holdArgs(why)...)...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return func() {
		cmd.Process.Kill()
		cmd.Wait()
	}, nil
}
