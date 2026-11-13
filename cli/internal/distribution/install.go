package distribution

import (
	"github.com/ClydeZhao/remote-agent-session/cli/internal/platformfs"

	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

type Installation struct {
	DSHPlugin     Artifact `json:"deepseek_harness,omitempty"`
	Schema        int      `json:"schema"`
	StateDir      string   `json:"state_dir,omitempty"`
	Prefix        string   `json:"prefix"`
	SkillDir      string   `json:"skill_dir"`
	Source        string   `json:"manifest_url"`
	AllowLoopback bool     `json:"allow_loopback_http,omitempty"`
	Version       string   `json:"version"`
	Revision      string   `json:"revision"`
	BinaryHash    string   `json:"binary_sha256"`
	SkillHash     string   `json:"skill_sha256"`
	References    string   `json:"skill_references,omitempty"`
}

func BinaryName() string {
	if runtime.GOOS == "windows" {
		return "remote-agent-session.exe"
	}
	return "remote-agent-session"
}
func (s Installation) root() string { return filepath.Join(s.Prefix, "share", "remote-agent-session") }
func (s Installation) paths() []string {
	return []string{filepath.Join(s.SkillDir, "SKILL.md"), filepath.Join(s.SkillDir, ".remote-agent-session-managed"), filepath.Join(s.Prefix, "bin", BinaryName()), filepath.Join(s.root(), ".managed"), filepath.Join(s.root(), "install.json")}
}
func safeDir(path string, create bool) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("installation path must be absolute and normalized")
	}
	for parent := path; parent != filepath.Dir(parent); parent = filepath.Dir(parent) {
		info, e := os.Lstat(parent)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 && !(runtime.GOOS == "darwin" && (parent == "/var" || parent == "/tmp" || parent == "/etc")) {
			return errors.New("symlink installation parent")
		}
	}
	if create {
		if e := platformfs.EnsureDir(path); e != nil {
			return e
		}
	}
	info, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !platformfs.Safe(path, info, false) {
		return errors.New("unsafe installation directory")
	}
	return nil
}
func regular(path string) error {
	info, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if !info.Mode().IsRegular() || !platformfs.Safe(path, info, false) {
		return errors.New("unsafe managed file")
	}
	return nil
}
func atomic(path string, data []byte, mode os.FileMode) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".remote-agent-session-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(mode); e == nil {
		_, e = f.Write(data)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	if e = platformfs.Replace(f.Name(), path); e != nil {
		return e
	}
	return platformfs.SyncDir(filepath.Dir(path))
}
func receipt(prefix string) (Installation, error) {
	var s Installation
	p := filepath.Join(prefix, "share", "remote-agent-session", "install.json")
	if regular(p) != nil {
		return s, errors.New("managed installation record unavailable; rerun the installer")
	}
	b, e := os.ReadFile(p)
	if e != nil || len(b) > 16384 || json.Unmarshal(b, &s) != nil || s.Schema != 1 || s.Prefix != prefix || ValidateURL(s.Source, s.AllowLoopback) != nil {
		return s, errors.New("invalid installation record")
	}
	if safeDir(s.root(), false) != nil || safeDir(filepath.Join(prefix, "bin"), false) != nil || safeDir(s.SkillDir, false) != nil {
		return s, errors.New("unsafe installation paths")
	}
	if _, err := referenceHashes(s.References); err != nil {
		return s, err
	}
	return s, nil
}
func Discover(executable string) (Installation, error) {
	path, e := filepath.Abs(executable)
	if e != nil {
		return Installation{}, e
	}
	if regular(path) != nil || filepath.Base(path) != BinaryName() || filepath.Base(filepath.Dir(path)) != "bin" {
		return Installation{}, errors.New("CLI is not in a managed installation")
	}
	return receipt(filepath.Dir(filepath.Dir(path)))
}
func (s Installation) Intact() bool {
	paths := s.paths()
	for _, p := range paths {
		if regular(p) != nil {
			return false
		}
	}
	b, e := os.ReadFile(paths[2])
	if e != nil || Digest(b) != s.BinaryHash {
		return false
	}
	b, e = os.ReadFile(paths[0])
	return e == nil && Digest(b) == s.SkillHash && s.referencesIntact()
}
func (s Installation) SkillIntact() bool {
	p := filepath.Join(s.SkillDir, "SKILL.md")
	if regular(p) != nil {
		return false
	}
	b, e := os.ReadFile(p)
	return e == nil && Digest(b) == s.SkillHash && s.referencesIntact()
}

type snapshot struct {
	Exists bool   `json:"exists"`
	Data   []byte `json:"data,omitempty"`
	Mode   uint32 `json:"mode"`
	SHA256 string `json:"sha256,omitempty"`
}
type transaction struct {
	Plan       Installation `json:"plan"`
	References []string     `json:"references,omitempty"`
	Files      []snapshot   `json:"files"`
}

func (s Installation) recover() error {
	p := filepath.Join(s.root(), "transaction.json")
	if _, e := os.Lstat(p); os.IsNotExist(e) {
		return nil
	}
	if regular(p) != nil {
		return errors.New("unsafe update recovery record")
	}
	b, e := os.ReadFile(p)
	var tx transaction
	if e != nil || len(b) > 96<<20 || json.Unmarshal(b, &tx) != nil || tx.Plan.Prefix != s.Prefix || tx.Plan.SkillDir != s.SkillDir || len(tx.Files) != len(s.paths())+len(tx.References) {
		return errors.New("invalid update recovery record")
	}
	paths, err := s.referencePaths(tx.References)
	if err != nil {
		return err
	}
	for _, old := range tx.Files {
		if old.Exists {
			if !validDigest(old.SHA256) || Digest(old.Data) != old.SHA256 || old.Mode > 0777 || old.Mode&0022 != 0 {
				return errors.New("damaged update recovery snapshot; installation preserved")
			}
		} else if len(old.Data) != 0 || old.Mode != 0 || old.SHA256 != "" {
			return errors.New("invalid absent recovery snapshot")
		}
	}
	if tx.Files[4].Exists {
		var previous Installation
		if json.Unmarshal(tx.Files[4].Data, &previous) != nil || previous.Schema != 1 || previous.Prefix != s.Prefix || previous.SkillDir != s.SkillDir || ValidateURL(previous.Source, previous.AllowLoopback) != nil || !tx.Files[2].Exists || !tx.Files[3].Exists || Digest(tx.Files[2].Data) != previous.BinaryHash {
			return errors.New("inconsistent update recovery metadata")
		}
		if _, err := version(previous.Version); err != nil || !validDigest(previous.SkillHash) || (tx.Plan.Version != "" && previous != tx.Plan) {
			return errors.New("inconsistent recovery version or source")
		}
	}
	for _, path := range paths {
		if _, e = os.Lstat(path); e == nil && regular(path) != nil {
			return errors.New("unsafe recovery destination")
		}
	}
	for i, path := range paths {
		old := tx.Files[i]
		if old.Exists {
			if e = atomic(path, old.Data, os.FileMode(old.Mode)); e != nil {
				return e
			}
		} else if e = os.Remove(path); e != nil && !os.IsNotExist(e) {
			return e
		}
	}
	return os.Remove(p)
}
func verifyBinary(path string, m Manifest, skill []byte) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--json", "version")
	out, e := cmd.Output()
	var result struct {
		Data map[string]string `json:"data"`
	}
	if e != nil || len(out) > 16384 || json.Unmarshal(out, &result) != nil || result.Data["version"] != m.Version || result.Data["revision"] != m.Revision || result.Data["api_version"] != m.APIVersion || result.Data["skill_sha256"] != Digest(skill) {
		return nil, errors.New("downloaded binary does not match release metadata")
	}
	if digest := result.Data["dsh_plugin_sha256"]; digest != "" && digest != m.Artifacts["deepseek-harness"].SHA256 {
		return nil, errors.New("plugin artifact does not match downloaded binary")
	}
	return candidateReferences(path, result.Data, skill)
}
func Bootstrap(prefix, skillDir, release, source string, loopback bool, binaryPath string, stateDirs ...string) (Installation, error) {
	s := Installation{Schema: 1, Prefix: filepath.Clean(prefix), SkillDir: filepath.Clean(skillDir), Source: source, AllowLoopback: loopback}
	if len(stateDirs) > 0 {
		s.StateDir = stateDirs[0]
	}
	if !filepath.IsAbs(prefix) || (skillDir != "" && !filepath.IsAbs(skillDir)) || ValidateURL(source, loopback) != nil || ValidateURL(release, loopback) != nil || !sameOrigin(source, release) {
		return s, errors.New("invalid installation options")
	}
	for _, p := range []string{s.Prefix, filepath.Join(s.Prefix, "share"), s.root(), filepath.Join(s.Prefix, "bin")} {
		if e := safeDir(p, true); e != nil {
			return s, e
		}
	}
	unlock, e := lock(s.root())
	if e != nil {
		return s, e
	}
	defer unlock()
	if skillDir == "" {
		if _, err := os.Lstat(filepath.Join(s.root(), "install.json")); err == nil {
			previous, err := receipt(s.Prefix)
			if err != nil {
				return s, err
			}
			s.SkillDir = previous.SkillDir
		} else if os.IsNotExist(err) {
			home, err := os.UserHomeDir()
			if err != nil {
				return s, err
			}
			s.SkillDir = filepath.Join(home, ".agents", "skills", "remote-agent-session")
		} else {
			return s, err
		}
	}
	if e = safeDir(s.SkillDir, true); e != nil {
		return s, e
	}
	if e = s.recover(); e != nil {
		return s, e
	}
	if _, e = os.Lstat(s.paths()[2]); e == nil {
		if regular(filepath.Join(s.root(), ".managed")) != nil {
			return s, errors.New("refusing to overwrite unmanaged CLI")
		}
	}
	if _, e = os.Lstat(filepath.Join(s.SkillDir, "SKILL.md")); e == nil && regular(filepath.Join(s.SkillDir, ".remote-agent-session-managed")) != nil {
		return s, errors.New("refusing to overwrite unmanaged skill")
	}
	var installedVersion string
	if _, e := os.Lstat(s.paths()[4]); e == nil {
		previous, err := receipt(s.Prefix)
		if err != nil {
			return s, err
		}
		installedVersion = previous.Version
		if s.StateDir == "" {
			s.StateDir = previous.StateDir
		}
		s.References = previous.References
		if previous.SkillDir != s.SkillDir {
			return s, errors.New("installation already uses a different skill directory")
		}
	}
	m, e := Check(release, loopback)
	if e != nil {
		return s, e
	}
	if release != source && Newer(installedVersion, m.Version) {
		return s, errors.New("a newer CLI is already installed")
	}
	binary, e := os.ReadFile(binaryPath)
	if e != nil {
		return s, e
	}
	if Digest(binary) != m.Artifacts[Platform()].SHA256 {
		return s, errors.New("bootstrap binary differs from manifest; retry with the current installer")
	}
	skill, e := resource(release, m.Artifacts["skill"], loopback, 1<<20)
	if e != nil {
		return s, e
	}
	return s.apply(m, binary, skill)
}
func Update(executable string) (Installation, error) {
	s, e := Discover(executable)
	if e != nil {
		return s, e
	}
	unlock, e := lock(s.root())
	if e != nil {
		return s, e
	}
	defer unlock()
	if e = s.recover(); e != nil {
		return s, e
	}
	s, e = receipt(s.Prefix)
	if e != nil {
		return s, e
	}
	if regular(s.paths()[2]) != nil || regular(s.paths()[1]) != nil || regular(s.paths()[3]) != nil {
		return s, errors.New("managed installation was replaced")
	}
	b, e := os.ReadFile(s.paths()[2])
	if e != nil || Digest(b) != s.BinaryHash {
		return s, errors.New("managed CLI was modified")
	}
	m, e := Check(s.Source, s.AllowLoopback)
	if e != nil {
		return s, e
	}
	if m.Version == s.Version {
		s, e = s.restoreDSHPluginRecord(m)
		if e != nil {
			return s, e
		}
	}
	if m.Version != s.Version && !Newer(m.Version, s.Version) {
		return s, errors.New("automatic downgrade is not allowed")
	}
	if m.Version == s.Version && (m.Revision != s.Revision || m.Artifacts[Platform()].SHA256 != s.BinaryHash || m.Artifacts["skill"].SHA256 != s.SkillHash || m.Artifacts["deepseek-harness"] != s.DSHPlugin) {
		return s, errors.New("published version was changed; use a new release version")
	}
	if m.Version == s.Version && s.SkillIntact() {
		return s, nil
	}
	binary, e := resource(s.Source, m.Artifacts[Platform()], s.AllowLoopback, 64<<20)
	if e != nil {
		return s, e
	}
	skill, e := resource(s.Source, m.Artifacts["skill"], s.AllowLoopback, 1<<20)
	if e != nil {
		return s, e
	}
	return s.apply(m, binary, skill)
}
func (s Installation) apply(m Manifest, binary, skill []byte) (Installation, error) {
	for _, p := range s.paths() {
		if _, e := os.Lstat(p); e == nil && regular(p) != nil {
			return s, errors.New("unsafe managed destination")
		}
	}
	f, e := os.CreateTemp(s.root(), ".candidate-*"+filepath.Ext(BinaryName()))
	if e != nil {
		return s, e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(binary); e != nil {
		f.Close()
		return s, e
	}
	f.Close()
	if e = os.Chmod(f.Name(), 0700); e != nil {
		return s, e
	}
	refs, e := verifyBinary(f.Name(), m, skill)
	if e != nil {
		return s, e
	}
	oldRefs, e := referenceHashes(s.References)
	if e != nil {
		return s, e
	}
	names := mergedReferenceNames(oldRefs, refs)
	paths, e := s.referencePaths(names)
	if e != nil {
		return s, e
	}
	for _, name := range names {
		path := filepath.Join(s.SkillDir, filepath.FromSlash(name))
		if _, err := os.Lstat(path); err == nil {
			if regular(path) != nil {
				return s, errors.New("unsafe reference destination")
			}
			if _, managed := oldRefs[name]; !managed {
				return s, errors.New("refusing to overwrite unmanaged reference")
			}
		} else if !os.IsNotExist(err) {
			return s, err
		}
	}
	tx := transaction{Plan: s, References: names}
	for _, p := range paths {
		info, e := os.Lstat(p)
		snap := snapshot{}
		if e == nil {
			snap.Exists = true
			snap.Mode = uint32(info.Mode().Perm())
			if runtime.GOOS == "windows" {
				snap.Mode = 0600
			}
			snap.Data, e = os.ReadFile(p)
			if e != nil {
				return s, e
			}
			snap.SHA256 = Digest(snap.Data)
		} else if !os.IsNotExist(e) {
			return s, e
		}
		tx.Files = append(tx.Files, snap)
	}
	journal := filepath.Join(s.root(), "transaction.json")
	j, _ := json.Marshal(tx)
	if e = atomic(journal, j, 0600); e != nil {
		return s, e
	}
	next := s
	next.Version = m.Version
	next.Revision = m.Revision
	next.BinaryHash = Digest(binary)
	next.SkillHash = Digest(skill)
	next.DSHPlugin = m.Artifacts["deepseek-harness"]
	hashes := map[string]string{}
	for name, data := range refs {
		hashes[name] = Digest([]byte(data))
	}
	next.References = ""
	if len(hashes) > 0 {
		encoded, _ := json.Marshal(hashes)
		next.References = string(encoded)
	}
	data, _ := json.Marshal(next)
	contents := [][]byte{skill, []byte("managed\n"), binary, []byte("managed\n"), data}
	for _, name := range names {
		if data, ok := refs[name]; ok {
			contents = append(contents, []byte(data))
		} else {
			contents = append(contents, nil)
		}
	}
	for i, p := range paths {
		mode := os.FileMode(0600)
		if i == 2 {
			mode = 0700
		}
		if contents[i] == nil {
			e = os.Remove(p)
			if os.IsNotExist(e) {
				e = nil
			}
		} else {
			e = atomic(p, contents[i], mode)
		}
		if e != nil {
			if s.recover() != nil {
				return s, errors.New("update failed; recovery pending, rerun installer")
			}
			return s, errors.New("update failed; previous installation restored")
		}
	}
	if e = os.Remove(journal); e != nil {
		return s, errors.New("update commit incomplete; rerun update to recover")
	}
	return next, nil
}
func (s Installation) Pending() bool {
	_, e := os.Lstat(filepath.Join(s.root(), "transaction.json"))
	return e == nil
}
