package localstate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "private"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPrivateCredentialsAndEndpointIsolation(t *testing.T) {
	s := newStore(t)
	secret := []byte("synthetic-credential-for-test")
	if err := s.SaveCredential("https://one.invalid", secret); err != nil {
		t.Fatal(err)
	}
	got, err := s.Credential("https://one.invalid")
	if err != nil || string(got) != string(secret) {
		t.Fatal("credential not recoverable")
	}
	if _, err = s.Credential("https://two.invalid"); err == nil {
		t.Fatal("credential crossed server boundary")
	}
	if err = s.SaveConfig(Config{Server: "https://one.invalid"}); err != nil {
		t.Fatal(err)
	}
	public, _ := os.ReadFile(filepath.Join(s.dir, "config.json"))
	if strings.Contains(string(public), string(secret)) {
		t.Fatal("credential leaked into config")
	}
	entries, _ := os.ReadDir(s.dir)
	for _, entry := range entries {
		info, _ := entry.Info()
		if info.Mode().Perm() != 0600 {
			t.Errorf("unsafe mode on %s", entry.Name())
		}
	}
	if err = s.ForgetCredential("https://one.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Credential("https://one.invalid"); err == nil {
		t.Fatal("credential still present")
	}
}

func TestRejectUnsafeStorage(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "public")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); !errors.Is(err, ErrUnsafe) {
		t.Fatal("public directory accepted")
	}
	s := newStore(t)
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte(`"synthetic"`), 0600); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(s.dir, "credential-"+digest([]byte("server"))+".json")
	if err := os.Symlink(outside, name); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Credential("server"); err == nil {
		t.Fatal("followed credential symlink")
	}
	if err := os.Remove(filepath.Join(s.dir, ".lock")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(s.dir, ".lock")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Config(); err == nil {
		t.Fatal("followed lock symlink")
	}
}

func TestConfigValidation(t *testing.T) {
	for _, url := range []string{"http://example.com", "https://user:secret@example.com", "https://example.com?secret=1", "https://example.com#secret", "http://localhost:8000", "file:///tmp/server"} {
		if _, err := ValidateServer(url, true); err == nil {
			t.Errorf("accepted unsafe URL %s", url)
		}
	}
	for _, url := range []string{"http://127.0.0.1:8000", "http://[::1]:8000"} {
		if _, err := ValidateServer(url, false); err == nil {
			t.Error("implicit cleartext accepted")
		}
		if _, err := ValidateServer(url, true); err != nil {
			t.Error(err)
		}
	}
	s := newStore(t)
	if err := s.SaveConfig(Config{Server: "https://example.com/api/"}); err != nil {
		t.Fatal(err)
	}
	c, err := s.Config()
	if err != nil || c.Server != "https://example.com/api" {
		t.Fatal("config not round tripped")
	}
	data, _ := json.Marshal(c)
	if strings.Contains(string(data), "credential") {
		t.Fatal("unexpected credential field")
	}
}
