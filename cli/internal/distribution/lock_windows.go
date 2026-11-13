package distribution

import (
	"errors"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/platformfs"
	"os"
	"path/filepath"
)

func lock(root string) (func(), error) {
	return platformfs.Lock(filepath.Join(root, ".update-lock"), true)
}

func serviceRegistryOwned(info os.FileInfo) bool { return false }
func serviceRegistryLock(root string) (func(), error) {
	return nil, errors.New("DSH state grants require macOS or Linux")
}
