//go:build darwin || linux

package localstate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func supportedPlatform() error { return nil }

func stateLockOpenError(err error) error {
	if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EROFS) {
		return fmt.Errorf("Remote Agent Session 无法写入本地状态锁。请检查原 Agent 是否允许访问该状态目录；若为宿主限制，请在原生权限流程中允许必要的目录写入，不要放宽文件权限或复制登录凭据：%w", err)
	}
	return ErrUnsafe
}

func (s *Store) locked(fn func() error) error {
	fd, err := syscall.Open(filepath.Join(s.dir, ".lock"), syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return stateLockOpenError(err)
	}
	f := os.NewFile(uintptr(fd), "local state lock")
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return ErrUnsafe
	}
	if syscall.Flock(fd, syscall.LOCK_EX) != nil {
		return errors.New("cannot lock local state")
	}
	defer syscall.Flock(fd, syscall.LOCK_UN)
	return fn()
}
