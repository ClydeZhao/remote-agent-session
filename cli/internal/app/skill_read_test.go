package app

import (
	"strings"
	"testing"
)

func TestSkillReferencesReadWithoutLogin(t *testing.T) {
	for _, topic := range []string{"setup", "session"} {
		code, out := runJSON(t, t.TempDir(), "skills", "read", topic)
		if code != 0 || !strings.Contains(out, "content") {
			t.Fatalf("%s: %d %s", topic, code, out)
		}
	}
	for _, topic := range []string{"../secret", "unknown", "/etc/passwd"} {
		code, _ := runJSON(t, t.TempDir(), "skills", "read", topic)
		if code == 0 {
			t.Fatal("invalid reference accepted", topic)
		}
	}
}
