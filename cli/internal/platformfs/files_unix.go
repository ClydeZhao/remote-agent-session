//go:build !windows

package platformfs

import "os"

func EnsureDir(path string) error { return os.MkdirAll(path, 0700) }

func Safe(path string, info os.FileInfo, private bool) bool {
	if private {
		return info.Mode().Perm()&0077 == 0
	}
	return info.Mode().Perm()&0022 == 0
}

func Replace(src, dst string) error { return os.Rename(src, dst) }

func SyncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
