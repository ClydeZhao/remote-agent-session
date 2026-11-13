//go:build darwin || linux

package localstate

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func (s *Store) RelayLock() (func(), error) {
	fd, err := syscall.Open(filepath.Join(s.dir, ".relay-lock"), syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return nil, stateLockOpenError(err)
	}
	f := os.NewFile(uintptr(fd), "relay lock")
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		f.Close()
		return nil, ErrUnsafe
	}
	if err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrRelayRunning
		}
		return nil, err
	}
	return func() { syscall.Flock(fd, syscall.LOCK_UN); f.Close() }, nil
}
