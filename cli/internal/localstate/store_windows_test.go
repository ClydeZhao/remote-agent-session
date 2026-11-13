package localstate

import (
	"os/exec"
	"testing"
)

func TestWindowsRejectsSharedState(t *testing.T) {
	s := newStore(t)
	if err := s.SaveCredential("test", []byte("synthetic")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Credential("test"); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("icacls.exe", s.dir, "/grant", "*S-1-1-0:(OI)(CI)R").CombinedOutput()
	if err != nil {
		t.Fatalf("set test ACL: %v %s", err, output)
	}
	if _, err := Open(s.dir); err == nil {
		t.Fatal("world-readable state accepted")
	}
}
