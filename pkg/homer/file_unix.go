//go:build !windows

package homer

import (
	"fmt"
	"os"
	"syscall"
)

// preserveOwner keeps the uid/gid the config file had before it was rewritten,
// so a Homer container running as a different user can still read it.
func preserveOwner(path string, fileStats os.FileInfo) error {
	fileSysStats, ok := fileStats.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	uid := int(fileSysStats.Uid)
	gid := int(fileSysStats.Gid)
	if err := os.Chown(path, uid, gid); err != nil {
		return fmt.Errorf("error chowning config '%s' with user '%v' and group '%v': %w", path, uid, gid, err)
	}
	return nil
}
