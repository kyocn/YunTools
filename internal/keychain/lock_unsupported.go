//go:build !darwin && !linux && !freebsd && !netbsd && !openbsd && !dragonfly && !windows

package keychain

import (
	"errors"
	"os"
)

func acquireLock(string) (*os.File, error) {
	return nil, errors.New("secure vault locking is unsupported on this platform")
}
func releaseLock(f *os.File) error { return f.Close() }
