package localstate

import (
	"errors"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/platformfs"
	"golang.org/x/sys/windows"
	"path/filepath"
)

func (s *Store) RelayLock() (func(), error) {
	unlock, err := platformfs.Lock(filepath.Join(s.dir, ".relay-lock"), true)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return nil, ErrRelayRunning
	}
	return unlock, err
}
