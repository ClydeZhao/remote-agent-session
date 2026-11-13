package distribution

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Artifact struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}
type Manifest struct {
	Schema       int                 `json:"schema"`
	Version      string              `json:"version"`
	Revision     string              `json:"revision"`
	APIVersion   string              `json:"api_version"`
	SkillVersion string              `json:"skill_version"`
	Artifacts    map[string]Artifact `json:"artifacts"`
}

func Platform() string       { return runtime.GOOS + "-" + runtime.GOARCH }
func Digest(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func validDigest(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && strings.ToLower(s) == s
}
func version(s string) ([3]int, error) {
	var v [3]int
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v, errors.New("invalid release version")
	}
	for i, p := range parts {
		n, e := strconv.Atoi(p)
		if e != nil || n < 0 || strconv.Itoa(n) != p {
			return v, errors.New("invalid release version")
		}
		v[i] = n
	}
	return v, nil
}
func Newer(target, current string) bool {
	t, e := version(target)
	if e != nil {
		return false
	}
	c, e := version(strings.TrimSuffix(current, "-dev"))
	if e != nil {
		return false
	}
	for i := 0; i < 3; i++ {
		if t[i] != c[i] {
			return t[i] > c[i]
		}
	}
	return strings.HasSuffix(current, "-dev")
}
func ValidateURL(raw string, loopback bool) error {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return errors.New("invalid release URL")
	}
	if u.Scheme != "https" && !(loopback && u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")) {
		return errors.New("release source requires HTTPS")
	}
	return nil
}
func sameOrigin(a, b string) bool {
	x, e := url.Parse(a)
	y, f := url.Parse(b)
	return e == nil && f == nil && x.Scheme == y.Scheme && x.Host == y.Host
}
func fetch(raw string, loopback bool, limit int64, timeout time.Duration) ([]byte, error) {
	if ValidateURL(raw, loopback) != nil {
		return nil, errors.New("invalid release source")
	}
	c := http.Client{Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return errors.New("release redirects are not followed")
	}}
	r, e := c.Get(raw)
	if e != nil {
		return nil, errors.New("release source unavailable")
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return nil, errors.New("release source returned an unexpected response")
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if e != nil || int64(len(b)) > limit {
		return nil, errors.New("release resource incomplete or too large")
	}
	return b, nil
}
func Check(source string, loopback bool) (Manifest, error) {
	var m Manifest
	b, e := fetch(source, loopback, 256<<10, 5*time.Second)
	if e != nil {
		return m, e
	}
	if json.Unmarshal(b, &m) != nil || m.Schema != 1 || m.SkillVersion != m.Version || !regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`).MatchString(m.Revision) || m.APIVersion == "" {
		return m, errors.New("invalid release manifest")
	}
	if _, e = version(m.Version); e != nil {
		return m, e
	}
	for _, name := range []string{Platform(), "skill"} {
		a, ok := m.Artifacts[name]
		if !ok || !validDigest(a.SHA256) || ValidateURL(a.URL, loopback) != nil || !sameOrigin(source, a.URL) {
			return m, errors.New("release artifact missing or invalid")
		}
	}
	if a, ok := m.Artifacts["deepseek-harness"]; ok {
		if !validDigest(a.SHA256) || ValidateURL(a.URL, loopback) != nil || !sameOrigin(source, a.URL) {
			return m, errors.New("DeepSeek Harness plugin artifact is invalid")
		}
	}
	return m, nil
}
func resource(source string, a Artifact, loopback bool, limit int64) ([]byte, error) {
	if !sameOrigin(source, a.URL) {
		return nil, errors.New("artifact origin differs from manifest")
	}
	b, e := fetch(a.URL, loopback, limit, 300*time.Second)
	if e != nil {
		return nil, e
	}
	if Digest(b) != a.SHA256 {
		return nil, errors.New("release checksum mismatch")
	}
	return b, nil
}
