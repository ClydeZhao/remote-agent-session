package localstate

import (
	"github.com/ClydeZhao/remote-agent-session/cli/internal/platformfs"
	"path/filepath"
)

func supportedPlatform() error { return nil }

func (s *Store) locked(fn func() error) error {
	unlock, err := platformfs.Lock(filepath.Join(s.dir, ".lock"), false)
	if err != nil {
		return err
	}
	defer unlock()
	return fn()
}
