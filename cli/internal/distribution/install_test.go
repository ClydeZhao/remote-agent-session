package distribution

import (
	"encoding/json"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/buildinfo"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

type releaseFixture struct {
	manifest      Manifest
	binary, skill []byte
	plugin        []byte
	corrupt       bool
}

func (f *releaseFixture) publish(version string) {
	f.skill = []byte("---\nname: remote-agent-session\ndescription: test fixture\n---\nrelease " + version + "\n")
	data, _ := json.Marshal(map[string]any{"data": map[string]string{"version": version, "revision": "fixture", "api_version": "0.3.0", "skill_sha256": Digest(f.skill)}})
	f.binary = []byte("#!/bin/sh\nprintf '%s\\n' '" + string(data) + "'\n")
	f.manifest = Manifest{Schema: 1, Version: version, Revision: "fixture", APIVersion: "0.3.0", SkillVersion: version, Artifacts: map[string]Artifact{}}
}
func fixtureServer(t *testing.T) (*releaseFixture, *httptest.Server) {
	t.Helper()
	f := &releaseFixture{}
	f.publish("1.0.0")
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest.json":
			json.NewEncoder(w).Encode(f.manifest)
		case "/binary":
			if f.corrupt {
				w.Write([]byte("corrupt"))
			} else {
				w.Write(f.binary)
			}
		case "/skill":
			w.Write(f.skill)
		case "/plugin":
			w.Write(f.plugin)
		default:
			http.NotFound(w, r)
		}
	}))
	f.manifest.Artifacts[Platform()] = Artifact{URL: s.URL + "/binary", SHA256: Digest(f.binary)}
	f.manifest.Artifacts["skill"] = Artifact{URL: s.URL + "/skill", SHA256: Digest(f.skill)}
	t.Cleanup(s.Close)
	return f, s
}
func advance(f *releaseFixture, s *httptest.Server, version string) {
	f.publish(version)
	f.manifest.Artifacts[Platform()] = Artifact{URL: s.URL + "/binary", SHA256: Digest(f.binary)}
	f.manifest.Artifacts["skill"] = Artifact{URL: s.URL + "/skill", SHA256: Digest(f.skill)}
}
func installFixture(t *testing.T, f *releaseFixture, s *httptest.Server) Installation {
	t.Helper()
	root := t.TempDir()
	candidate := filepath.Join(root, "candidate")
	os.WriteFile(candidate, f.binary, 0700)
	prefix := filepath.Join(root, "prefix")
	skill := filepath.Join(root, "skill")
	result, e := Bootstrap(prefix, skill, s.URL+"/latest.json", s.URL+"/latest.json", true, candidate)
	if e != nil {
		t.Fatal(e)
	}
	return result
}
func TestInstallUpdatePreservesUnrelatedStateAndRepairsSkill(t *testing.T) {
	f, server := fixtureServer(t)
	s := installFixture(t, f, server)
	unrelated := filepath.Join(s.Prefix, "session-do-not-touch")
	os.WriteFile(unrelated, []byte("credential/request"), 0600)
	advance(f, server, "1.1.0")
	got, e := Update(s.paths()[2])
	if e != nil {
		t.Fatal(e)
	}
	if got.Version != "1.1.0" || !got.Intact() {
		t.Fatal("release did not update coherently")
	}
	b, _ := os.ReadFile(unrelated)
	if string(b) != "credential/request" {
		t.Fatal("unrelated state changed")
	}
	os.WriteFile(got.paths()[0], []byte("old skill"), 0600)
	repaired, e := Update(s.paths()[2])
	if e != nil || !repaired.Intact() {
		t.Fatal("same-version skill repair failed", e)
	}
}
func TestCorruptDownloadAndDowngradeLeaveInstallationUnchanged(t *testing.T) {
	f, server := fixtureServer(t)
	s := installFixture(t, f, server)
	old, _ := os.ReadFile(s.paths()[2])
	advance(f, server, "1.1.0")
	f.corrupt = true
	if _, e := Update(s.paths()[2]); e == nil {
		t.Fatal("corrupt artifact accepted")
	}
	b, _ := os.ReadFile(s.paths()[2])
	if string(b) != string(old) || !s.Intact() {
		t.Fatal("failed update changed installation")
	}
	advance(f, server, "0.9.0")
	f.corrupt = false
	if _, e := Update(s.paths()[2]); e == nil {
		t.Fatal("downgrade accepted")
	}
	if !s.Intact() {
		t.Fatal("downgrade changed installation")
	}
}
func TestInterruptedReplacementRestoresBeforeNetworkFailure(t *testing.T) {
	f, server := fixtureServer(t)
	s := installFixture(t, f, server)
	tx := transaction{Plan: s}
	for _, p := range s.paths() {
		b, _ := os.ReadFile(p)
		info, _ := os.Stat(p)
		tx.Files = append(tx.Files, snapshot{Exists: true, Data: b, Mode: uint32(info.Mode().Perm()), SHA256: Digest(b)})
	}
	data, _ := json.Marshal(tx)
	os.WriteFile(filepath.Join(s.root(), "transaction.json"), data, 0600)
	os.WriteFile(s.paths()[0], []byte("half-updated skill"), 0600)
	server.Close()
	if _, e := Update(s.paths()[2]); e == nil {
		t.Fatal("offline update unexpectedly succeeded")
	}
	if !s.Intact() || s.Pending() {
		t.Fatal("interrupted install not restored before failed fetch")
	}
}
func TestUnmanagedAndSymlinkPathsRejected(t *testing.T) {
	f, server := fixtureServer(t)
	root := t.TempDir()
	prefix := filepath.Join(root, "prefix")
	skill := filepath.Join(root, "skill")
	os.MkdirAll(filepath.Join(prefix, "bin"), 0700)
	os.MkdirAll(skill, 0700)
	binary := filepath.Join(prefix, "bin", "remote-agent-session")
	os.WriteFile(binary, []byte("personal binary"), 0700)
	candidate := filepath.Join(root, "candidate")
	os.WriteFile(candidate, f.binary, 0700)
	if _, e := Bootstrap(prefix, skill, server.URL+"/latest.json", server.URL+"/latest.json", true, candidate); e == nil {
		t.Fatal("unmanaged CLI overwritten")
	}
	b, _ := os.ReadFile(binary)
	if string(b) != "personal binary" {
		t.Fatal("unmanaged binary changed")
	}
	link := filepath.Join(root, "link")
	os.Symlink(prefix, link)
	if _, e := Bootstrap(filepath.Join(link, "nested"), skill, server.URL+"/latest.json", server.URL+"/latest.json", true, candidate); e == nil {
		t.Fatal("symlink ancestor followed")
	}
}
func TestManifestOriginRedirectAndMetadataValidation(t *testing.T) {
	f, server := fixtureServer(t)
	for _, mutate := range []func(){func() { f.manifest.Schema = 2 }, func() { f.manifest.SkillVersion = "other" }, func() {
		f.manifest.Artifacts["skill"] = Artifact{URL: "https://other.invalid/skill", SHA256: Digest(f.skill)}
	}, func() { f.manifest.Version = "opaque" }} {
		advance(f, server, "1.0.0")
		mutate()
		if _, e := Check(server.URL+"/latest.json", true); e == nil {
			t.Fatal("invalid manifest accepted")
		}
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, server.URL+"/latest.json", 302) }))
	defer redirect.Close()
	if _, e := Check(redirect.URL, true); e == nil {
		t.Fatal("redirect followed")
	}
	if _, e := Check(server.URL+"/latest.json", false); e == nil {
		t.Fatal("HTTP source accepted without explicit loopback mode")
	}
}

func TestDamagedRecoveryCannotReplaceWorkingBinary(t *testing.T) {
	f, server := fixtureServer(t)
	s := installFixture(t, f, server)
	original, _ := os.ReadFile(s.paths()[2])
	tx := transaction{Plan: s}
	for _, p := range s.paths() {
		b, _ := os.ReadFile(p)
		info, _ := os.Stat(p)
		tx.Files = append(tx.Files, snapshot{Exists: true, Data: b, Mode: uint32(info.Mode().Perm()), SHA256: Digest(b)})
	}
	tx.Files[2].Data = []byte("corrupt recovery bytes")
	data, _ := json.Marshal(tx)
	journal := filepath.Join(s.root(), "transaction.json")
	os.WriteFile(journal, data, 0600)
	if _, e := Update(s.paths()[2]); e == nil {
		t.Fatal("corrupt recovery accepted")
	}
	actual, _ := os.ReadFile(s.paths()[2])
	if string(actual) != string(original) || !s.Pending() {
		t.Fatal("damaged journal replaced working installation or disappeared")
	}
}

func publishReferences(f *releaseFixture, server *httptest.Server, version string, refs map[string]string) {
	advance(f, server, version)
	files := map[string]string{"SKILL.md": string(f.skill)}
	for name, data := range refs {
		files[name] = data
	}
	info, _ := json.Marshal(map[string]any{"data": map[string]string{"version": version, "revision": "fixture", "api_version": "0.3.0", "skill_sha256": Digest(f.skill), "skill_references_sha256": buildinfo.ReferenceDigest(files)}})
	bundle, _ := json.Marshal(map[string]any{"data": map[string]any{"files": files}})
	f.binary = []byte("#!/bin/sh\ncase \"$*\" in\n*'skills export'*) printf '%s\\n' '" + string(bundle) + "';;\n*) printf '%s\\n' '" + string(info) + "';;\nesac\n")
	f.manifest.Artifacts[Platform()] = Artifact{URL: server.URL + "/binary", SHA256: Digest(f.binary)}
}

func TestReferenceInstallUpgradeRepairAndRemoval(t *testing.T) {
	f, server := fixtureServer(t)
	installed := installFixture(t, f, server)
	publishReferences(f, server, "1.1.0", map[string]string{"references/guide.md": "setup one", "references/session.md": "session one"})
	next, err := Update(installed.paths()[2])
	if err != nil {
		t.Fatal(err)
	}
	if !next.SkillIntact() {
		t.Fatal("references not installed")
	}
	path := filepath.Join(next.SkillDir, "references", "guide.md")
	if err = os.WriteFile(path, []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	if next.SkillIntact() {
		t.Fatal("reference damage not detected")
	}
	repaired, err := Update(next.paths()[2])
	if err != nil || !repaired.Intact() {
		t.Fatal("repair failed", err)
	}
	candidate := filepath.Join(t.TempDir(), "candidate")
	os.WriteFile(candidate, f.binary, 0700)
	if _, err = Bootstrap(next.Prefix, next.SkillDir, server.URL+"/latest.json", server.URL+"/latest.json", true, candidate); err != nil {
		t.Fatal("reinstall failed", err)
	}
	unrelated := filepath.Join(next.SkillDir, "references", "personal.md")
	os.WriteFile(unrelated, []byte("personal"), 0600)
	publishReferences(f, server, "1.2.0", map[string]string{"references/guide.md": "setup two"})
	next, err = Update(next.paths()[2])
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(next.SkillDir, "references", "session.md")); !os.IsNotExist(err) {
		t.Fatal("obsolete reference retained")
	}
	if b, _ := os.ReadFile(unrelated); string(b) != "personal" {
		t.Fatal("unmanaged reference modified")
	}
	if b, _ := os.ReadFile(path); string(b) != "setup two" {
		t.Fatal("reference not upgraded")
	}
	if !next.Intact() {
		t.Fatal("installation not intact")
	}
}

func TestReferenceRecoveryRestoresAllManagedFiles(t *testing.T) {
	f, server := fixtureServer(t)
	publishReferences(f, server, "1.0.0", map[string]string{"references/guide.md": "original"})
	s := installFixture(t, f, server)
	names := []string{"references/guide.md", "references/new.md"}
	paths, err := s.referencePaths(names)
	if err != nil {
		t.Fatal(err)
	}
	tx := transaction{Plan: s, References: names}
	for _, path := range paths {
		snap := snapshot{}
		if b, e := os.ReadFile(path); e == nil {
			snap = snapshot{Exists: true, Data: b, Mode: 0600, SHA256: Digest(b)}
		}
		tx.Files = append(tx.Files, snap)
	}
	tx.Files[2].Mode = 0700
	journal, _ := json.Marshal(tx)
	os.WriteFile(filepath.Join(s.root(), "transaction.json"), journal, 0600)
	os.WriteFile(paths[5], []byte("partial"), 0600)
	os.WriteFile(paths[6], []byte("new"), 0600)
	if err = s.recover(); err != nil {
		t.Fatal(err)
	}
	if !s.Intact() {
		t.Fatal("references not recovered")
	}
	if _, err = os.Stat(paths[6]); !os.IsNotExist(err) {
		t.Fatal("new reference not rolled back")
	}
}

func TestUnsafeOrUnmanagedReferenceRejected(t *testing.T) {
	for _, name := range []string{"../escape.md", "references/guide.md"} {
		t.Run(name, func(t *testing.T) {
			f, server := fixtureServer(t)
			s := installFixture(t, f, server)
			dir := filepath.Join(s.SkillDir, "references")
			os.Mkdir(dir, 0700)
			path := filepath.Join(dir, "guide.md")
			os.WriteFile(path, []byte("personal"), 0600)
			publishReferences(f, server, "1.1.0", map[string]string{name: "replace"})
			if _, err := Update(s.paths()[2]); err == nil {
				t.Fatal("unsafe/unmanaged reference accepted")
			}
			if b, _ := os.ReadFile(path); string(b) != "personal" {
				t.Fatal("personal reference overwritten")
			}
			if !s.Intact() {
				t.Fatal("old install changed")
			}
		})
	}
}

func TestLegacyReceiptCannotHideMissingCurrentReferences(t *testing.T) {
	oldVersion, oldRevision := buildinfo.Version, buildinfo.Revision
	defer func() { buildinfo.Version = oldVersion; buildinfo.Revision = oldRevision }()
	buildinfo.Version = "1.0.0"
	buildinfo.Revision = "fixture"
	f, server := fixtureServer(t)
	s := installFixture(t, f, server)
	if s.SkillIntact() {
		t.Fatal("current split skill with legacy receipt reported intact")
	}
}
