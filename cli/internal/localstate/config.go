package localstate

import (
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

var ErrUnconfigured = errors.New("server is not configured; use config set")

type Config struct {
	Server            string `json:"server"`
	AllowLoopbackHTTP bool   `json:"allow_loopback_http"`
}

func DefaultDir() (string, error) {
	if value := os.Getenv("REMOTE_AGENT_SESSION_STATE_DIR"); value != "" {
		return filepath.Abs(value)
	}
	base, err := os.UserHomeDir()
	if err != nil {
		return "", errors.New("cannot locate user configuration directory; use --state-dir")
	}
	return filepath.Join(base, ".remote-agent-session"), nil
}

func ValidateServer(raw string, allowLoopback bool) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return "", errors.New("server must be an HTTPS URL without credentials, query or fragment")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || !allowLoopback || ip == nil || !ip.IsLoopback() {
			return "", errors.New("HTTPS is required; explicit loopback HTTP is only for isolated development")
		}
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func (s *Store) SaveConfig(c Config) error {
	server, err := ValidateServer(c.Server, c.AllowLoopbackHTTP)
	if err != nil {
		return err
	}
	c.Server = server
	return s.locked(func() error { return s.write("config.json", c) })
}

func (s *Store) Config() (c Config, err error) {
	err = s.locked(func() error {
		if e := s.read("config.json", &c); e != nil {
			if errors.Is(e, os.ErrNotExist) {
				c = Config{Server: "http://127.0.0.1:18481", AllowLoopbackHTTP: true}
				return nil
			}
			return errors.New("cannot load private configuration")
		}
		server, e := ValidateServer(c.Server, c.AllowLoopbackHTTP)
		if e != nil {
			return e
		}
		c.Server = server
		return nil
	})
	return
}
