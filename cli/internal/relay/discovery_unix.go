//go:build !windows

package relay

import (
	"os"
	"syscall"
)

func validatePrivateDiscoveryPath(path string, directory bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return ErrInterruptUnavailable
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Getuid()) || info.Mode().Perm()&0077 != 0 || info.Mode()&os.ModeSymlink != 0 {
		return ErrInterruptUnavailable
	}
	if directory {
		if !info.IsDir() {
			return ErrInterruptUnavailable
		}
	} else if !info.Mode().IsRegular() || info.Size() > 4096 || stat.Nlink != 1 {
		return ErrInterruptUnavailable
	}
	return nil
}
