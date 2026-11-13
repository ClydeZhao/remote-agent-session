package distribution

import (
	"github.com/ClydeZhao/remote-agent-session/cli/internal/buildinfo"
	"os"
	"path/filepath"
	"testing"
)

func TestDSHPluginUsesInstalledReleaseAfterLatestAdvances(t *testing.T) {
	f, server := fixtureServer(t)
	f.plugin = []byte("matching immutable plugin")
	f.manifest.Artifacts["deepseek-harness"] = Artifact{URL: server.URL + "/plugin", SHA256: Digest(f.plugin)}
	s := installFixture(t, f, server)
	advance(f, server, "2.0.0")
	path, err := PrepareDSHPlugin(s.paths()[2])
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "matching immutable plugin" {
		t.Fatal("installed version was not pinned", err)
	}
	server.Close()
	if cached, err := PrepareDSHPlugin(s.paths()[2]); err != nil || cached != path {
		t.Fatal("verified cached archive unavailable offline", err)
	}
}

func TestDSHPluginRejectsChangedBytesAndUnsafeCache(t *testing.T) {
	f, server := fixtureServer(t)
	f.plugin = []byte("expected")
	f.manifest.Artifacts["deepseek-harness"] = Artifact{URL: server.URL + "/plugin", SHA256: Digest(f.plugin)}
	s := installFixture(t, f, server)
	f.plugin = []byte("changed")
	if _, err := PrepareDSHPlugin(s.paths()[2]); err == nil {
		t.Fatal("changed archive accepted")
	}
	path := filepath.Join(s.root(), "plugins", s.DSHPlugin.SHA256+".tgz")
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("failed download created cached archive")
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	f.plugin = []byte("expected")
	if _, err := PrepareDSHPlugin(s.paths()[2]); err == nil {
		t.Fatal("symlink cache accepted")
	}
	data, _ := os.ReadFile(outside)
	if string(data) != "keep" {
		t.Fatal("unrelated file modified")
	}
}

func TestDSHPluginRejectsForeignOriginAndLegacyRelease(t *testing.T) {
	f, server := fixtureServer(t)
	s := installFixture(t, f, server)
	if _, err := PrepareDSHPlugin(s.paths()[2]); err == nil {
		t.Fatal("legacy release invented a plugin")
	}
	f.manifest.Artifacts["deepseek-harness"] = Artifact{URL: "https://other.invalid/plugin", SHA256: Digest([]byte("plugin"))}
	if _, err := Check(server.URL+"/latest.json", true); err == nil {
		t.Fatal("foreign plugin origin accepted")
	}
}

func TestSameVersionPluginChangeIsRejectedDuringSkillRepair(t *testing.T) {
	f, server := fixtureServer(t)
	f.plugin = []byte("original plugin")
	f.manifest.Artifacts["deepseek-harness"] = Artifact{URL: server.URL + "/plugin", SHA256: Digest(f.plugin)}
	s := installFixture(t, f, server)
	original := s.DSHPlugin
	if err := os.WriteFile(s.paths()[0], []byte("damaged skill"), 0600); err != nil {
		t.Fatal(err)
	}
	f.plugin = []byte("changed plugin under same version")
	f.manifest.Artifacts["deepseek-harness"] = Artifact{URL: server.URL + "/plugin", SHA256: Digest(f.plugin)}
	if _, err := Update(s.paths()[2]); err == nil {
		t.Fatal("same-version plugin replacement accepted during repair")
	}
	current, err := Discover(s.paths()[2])
	if err != nil || current.DSHPlugin != original {
		t.Fatal("pinned plugin changed after failed update", err)
	}
}

func TestNewReleaseUpdatesPluginPinAndRetainsNativeArchiveReference(t *testing.T) {
	f, server := fixtureServer(t)
	f.plugin = []byte("old plugin")
	f.manifest.Artifacts["deepseek-harness"] = Artifact{URL: server.URL + "/plugin", SHA256: Digest(f.plugin)}
	s := installFixture(t, f, server)
	oldPath, err := PrepareDSHPlugin(s.paths()[2])
	if err != nil {
		t.Fatal(err)
	}
	advance(f, server, "1.1.0")
	f.plugin = []byte("new plugin")
	f.manifest.Artifacts["deepseek-harness"] = Artifact{URL: server.URL + "/plugin", SHA256: Digest(f.plugin)}
	updated, err := Update(s.paths()[2])
	if err != nil || !updated.Intact() || updated.DSHPlugin != f.manifest.Artifacts["deepseek-harness"] {
		t.Fatal("new release plugin pin lost", err)
	}
	newPath, err := PrepareDSHPlugin(s.paths()[2])
	if err != nil || newPath == oldPath {
		t.Fatal("new plugin archive not selected", err)
	}
	old, err := os.ReadFile(oldPath)
	if err != nil || string(old) != "old plugin" {
		t.Fatal("existing native package reference destroyed", err)
	}
}

func TestLegacyPluginReceiptRequiresBinaryPinnedArtifact(t *testing.T) {
	f, server := fixtureServer(t)
	s := installFixture(t, f, server)
	version, revision, digest := buildinfo.Version, buildinfo.Revision, buildinfo.DSHPluginSHA256
	defer func() { buildinfo.Version = version; buildinfo.Revision = revision; buildinfo.DSHPluginSHA256 = digest }()
	buildinfo.Version = s.Version
	buildinfo.Revision = s.Revision
	buildinfo.DSHPluginSHA256 = Digest([]byte("compiled plugin"))
	f.manifest.Artifacts["deepseek-harness"] = Artifact{URL: server.URL + "/plugin", SHA256: Digest([]byte("different"))}
	if _, err := s.restoreDSHPluginRecord(f.manifest); err == nil {
		t.Fatal("legacy metadata adopted an unpinned plugin")
	}
	current, err := Discover(s.paths()[2])
	if err != nil || current.DSHPlugin != (Artifact{}) {
		t.Fatal("failed metadata recovery changed receipt", err)
	}
	f.manifest.Artifacts["deepseek-harness"] = Artifact{URL: server.URL + "/plugin", SHA256: buildinfo.DSHPluginSHA256}
	restored, err := s.restoreDSHPluginRecord(f.manifest)
	if err != nil || restored.DSHPlugin != f.manifest.Artifacts["deepseek-harness"] {
		t.Fatal("matching legacy record not recovered", err)
	}
	persisted, err := Discover(s.paths()[2])
	if err != nil || persisted.DSHPlugin != restored.DSHPlugin {
		t.Fatal("recovered pin not durable", err)
	}
}

func TestPackagedDSHPluginUsesContentAddressedInstallPath(t *testing.T) {
	root := t.TempDir()
	share := filepath.Join(root, "share", "remote-agent-session")
	if err := os.MkdirAll(share, 0700); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(share, "deepseek-harness.tgz")
	binary := filepath.Join(root, "bin", "remote-agent-session")
	original := buildinfo.DSHPluginSHA256
	t.Cleanup(func() { buildinfo.DSHPluginSHA256 = original })
	var previous string
	for _, content := range []string{"first packaged plugin", "new contents same package version"} {
		if err := os.WriteFile(archive, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		buildinfo.DSHPluginSHA256 = Digest([]byte(content))
		if _, err := PackagedDSHPlugin(binary); err != nil {
			t.Fatal(err)
		}
		path, err := PrepareDSHPlugin(binary)
		if err != nil {
			t.Fatal(err)
		}
		if path == archive || path == previous || filepath.Base(path) != buildinfo.DSHPluginSHA256+".tgz" {
			t.Fatal("plugin manager could reuse stale same-version archive", path)
		}
		previous = path
	}
}
