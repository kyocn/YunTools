//go:build darwin || linux || freebsd || netbsd || openbsd || dragonfly

package keychain

import (
	"errors"
	"os"
	"syscall"
)

func acquireLock(path string) (*os.File, error) {
	fd, e := syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), path)
	if e = f.Chmod(0600); e != nil {
		f.Close()
		return nil, e
	}
	if e = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		if errors.Is(e, syscall.EWOULDBLOCK) || errors.Is(e, syscall.EAGAIN) {
			return nil, ErrLocked
		}
		return nil, e
	}
	return f, nil
}
func releaseLock(f *os.File) error {
	e := syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	closeErr := f.Close()
	if e != nil {
		return e
	}
	return closeErr
}
