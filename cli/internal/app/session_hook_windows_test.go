package app

import "testing"

func TestWindowsHookCommandQuotesPaths(t *testing.T) {
	command, err := formatRelayHookCommand(`C:\Users\Test & Co\agent.exe`, `C:\state dir\`)
	if err != nil {
		t.Fatal(err)
	}
	if command != `"C:\Users\Test & Co\agent.exe" --state-dir "C:\state dir\\" session hook` {
		t.Fatalf("unexpected command %q", command)
	}
	for _, unsafe := range []string{"percent%PATH%", "delayed!PATH!", "line\nnext", "quote\"", "nul\x00"} {
		if _, err := formatRelayHookCommand(`C:\agent.exe`, unsafe); err == nil {
			t.Fatalf("accepted cmd expansion/control characters %q", unsafe)
		}
	}
}
