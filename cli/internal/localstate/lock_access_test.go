//go:build darwin || linux

package localstate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadOnlyStateLockReportsAccessRestriction(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires an unprivileged user")
	}
	for _, relay := range []bool{false, true} {
		s := newStore(t)
		name := ".lock"
		if relay {
			name = ".relay-lock"
		}
		path := filepath.Join(s.dir, name)
		if err := os.WriteFile(path, nil, 0400); err != nil {
			t.Fatal(err)
		}
		var err error
		if relay {
			var unlock func()
			unlock, err = s.RelayLock()
			if unlock != nil {
				unlock()
			}
		} else {
			_, err = s.Config()
		}
		if !errors.Is(err, os.ErrPermission) || errors.Is(err, ErrUnsafe) {
			t.Fatalf("access denial was misclassified as unsafe permissions: %v", err)
		}
		info, statErr := os.Stat(path)
		if statErr != nil || info.Mode().Perm() != 0400 {
			t.Fatal("lock permissions modified")
		}
	}
}
