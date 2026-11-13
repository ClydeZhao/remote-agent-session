//go:build darwin || linux

package distribution

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func lock(root string) (func(), error) {
	fd, e := syscall.Open(filepath.Join(root, ".update-lock"), syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), "update lock")
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		f.Close()
		return nil, errors.New("unsafe update lock")
	}
	if e = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, errors.New("another installer or update is running")
	}
	return func() { syscall.Flock(fd, syscall.LOCK_UN); f.Close() }, nil
}

func serviceRegistryOwned(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Getuid()) && info.Mode().Perm()&0077 == 0 && (info.IsDir() || stat.Nlink == 1)
}

func serviceRegistryLock(root string) (func(), error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || !serviceRegistryOwned(info) {
		return nil, errors.New("unsafe service registry directory")
	}
	fd, err := syscall.Open(filepath.Join(root, "service-roots.lock"), syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "service registry lock")
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() || !serviceRegistryOwned(info) {
		file.Close()
		return nil, errors.New("unsafe service registry lock")
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("service state or host permissions are changing; retry this reviewed plan")
	}
	return func() { syscall.Flock(fd, syscall.LOCK_UN); file.Close() }, nil
}
