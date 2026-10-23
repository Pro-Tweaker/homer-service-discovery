//go:build windows

package homer

import "os"

// preserveOwner is a no-op on Windows, which has no uid/gid file ownership.
func preserveOwner(string, os.FileInfo) error {
	return nil
}
