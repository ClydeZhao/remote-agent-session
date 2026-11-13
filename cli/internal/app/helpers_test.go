package app

import (
	"bytes"
	"encoding/json"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/localstate"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

const testID = "11111111-1111-4111-8111-111111111111"
const testToken = "synthetic-device-token"

func jsonReply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func testSetup(t *testing.T, handler http.HandlerFunc) (string, *localstate.Store) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	dir := filepath.Join(t.TempDir(), "state")
	s, err := localstate.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveConfig(localstate.Config{Server: srv.URL, AllowLoopbackHTTP: true}); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveCredential(srv.URL, []byte(testToken)); err != nil {
		t.Fatal(err)
	}
	return dir, s
}
func runJSON(t *testing.T, dir string, args ...string) (int, string) {
	t.Helper()
	var out, stderr bytes.Buffer
	exit := Run(append([]string{"--json", "--state-dir", dir}, args...), &out, &stderr)
	if !json.Valid(out.Bytes()) {
		t.Fatalf("invalid JSON: %q", out.String())
	}
	if strings.Contains(out.String(), testToken) {
		t.Fatal("token leaked")
	}
	if stderr.Len() != 0 {
		t.Fatal("unexpected progress", stderr.String())
	}
	return exit, out.String()
}
