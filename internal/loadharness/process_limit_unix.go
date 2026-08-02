//go:build darwin || linux

package loadharness

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func validateProcessCapacity(sessions int) error {
	var limit unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &limit); err != nil {
		return fmt.Errorf("read open-file limit: %w", err)
	}
	required := minimumOpenFiles(sessions)
	if limit.Cur < uint64(required) {
		return fmt.Errorf("open-file soft limit %d is below required %d for %d sessions; raise it before running the load", limit.Cur, required, sessions)
	}
	return nil
}
