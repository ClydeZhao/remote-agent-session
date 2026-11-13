package localstate

import (
	"path/filepath"
	"testing"
	"time"
)

func TestUpdateRefreshIsThrottledAndSourceScoped(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "state"))
	if e != nil {
		t.Fatal(e)
	}
	if !s.ClaimUpdateRefresh("source-a") || s.ClaimUpdateRefresh("source-a") {
		t.Fatal("concurrent commands can repeatedly refresh")
	}
	if !s.ClaimUpdateRefresh("source-b") {
		t.Fatal("changed source reused unrelated cache")
	}
	if e = s.SaveUpdateCache(UpdateCache{Source: "source-b", Latest: "1.1.0", CheckedAt: time.Now().Add(-25 * time.Hour)}); e != nil {
		t.Fatal(e)
	}
	if !s.ClaimUpdateRefresh("source-b") {
		t.Fatal("stale cache never refreshed")
	}
}
