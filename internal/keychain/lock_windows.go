//go:build windows

package keychain

import (
	"os"
	"syscall"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")
var lockFileEx = kernel32.NewProc("LockFileEx")
var unlockFileEx = kernel32.NewProc("UnlockFileEx")

func acquireLock(path string) (*os.File, error) {
	if info, e := os.Lstat(path); e == nil && !info.Mode().IsRegular() {
		return nil, ErrLocked
	} else if e != nil && !os.IsNotExist(e) {
		return nil, e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	var overlapped syscall.Overlapped
	ok, _, e := lockFileEx.Call(f.Fd(), 3, 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	if ok == 0 {
		f.Close()
		if e == syscall.Errno(33) {
			return nil, ErrLocked
		}
		return nil, e
	}
	return f, nil
}
func releaseLock(f *os.File) error {
	var overlapped syscall.Overlapped
	ok, _, e := unlockFileEx.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	ce := f.Close()
	if ok == 0 {
		return e
	}
	return ce
}
