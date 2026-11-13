//go:build !darwin && !linux && !windows

package distribution

import (
	"errors"
	"os"
)

func lock(root string) (func(), error) {
	return nil, errors.New("managed updates are not supported on this platform")
}

func serviceRegistryOwned(info os.FileInfo) bool { return false }
func serviceRegistryLock(root string) (func(), error) {
	return nil, errors.New("DSH state grants require macOS or Linux")
}
