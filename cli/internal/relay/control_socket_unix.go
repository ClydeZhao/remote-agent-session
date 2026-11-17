//go:build !windows

package relay

import (
	"os"
	"path/filepath"
	"syscall"
)

func validateControlSocket(path string) error {
	if !filepath.IsAbs(path) {
		return ErrInterruptUnavailable
	}
	for _, name := range []string{filepath.Dir(path), path} {
		info, err := os.Lstat(name)
		if err != nil {
			return ErrInterruptUnavailable
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != uint32(os.Getuid()) || info.Mode().Perm()&0022 != 0 || info.Mode()&os.ModeSymlink != 0 {
			return ErrInterruptUnavailable
		}
	}

	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		return ErrInterruptUnavailable
	}
	return nil
}

func prepareControlSocket(path string) (string, func(), error) {
	if !filepath.IsAbs(path) {
		return "", nil, ErrInterruptUnavailable
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", nil, ErrInterruptUnavailable
	}
	if err := validateControlSocket(resolved); err != nil {
		return "", nil, err
	}
	return resolved, func() {}, nil
}
