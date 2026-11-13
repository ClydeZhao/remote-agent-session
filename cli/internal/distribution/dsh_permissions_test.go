package distribution

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func dshPermissionsFixture(t *testing.T) DSHPermissionInput {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	userHome := filepath.Join(root, "user-home")
	if err := os.MkdirAll(userHome, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", userHome)
	home := filepath.Join(root, "dsh")
	profile := filepath.Join(home, "profiles", "desktop")
	if err := os.MkdirAll(profile, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "cordis.patch.yml"), []byte("- id: other\n  config:\n    name: keep\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return DSHPermissionInput{Profile: "desktop", ProfileDirectory: profile, Home: home, StateDir: filepath.Join(root, "state"), Executable: filepath.Join(root, "bin", "remote-agent-session"), Environment: "dev", Operation: "install", EffectiveRoots: []string{}}
}

func TestDSHPermissionsPlanDoesNotWriteAndRequiresExactReview(t *testing.T) {
	input := dshPermissionsFixture(t)
	plan, err := PlanDSHPermissions(input)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Changes || plan.Status != "review_required" || plan.Profile != "desktop" || plan.Environment != "dev" || plan.StateDir != input.StateDir || len(plan.ID) != 64 {
		t.Fatal("incomplete review", plan)
	}
	if _, err := os.Stat(input.StateDir); !os.IsNotExist(err) {
		t.Fatal("plan created state")
	}
	if err := ApplyDSHPermissions(plan, "different-review"); err == nil {
		t.Fatal("unreviewed scope accepted")
	}
	if _, err := os.Stat(filepath.Join(input.ProfileDirectory, dshPermissionRecordName)); !os.IsNotExist(err) {
		t.Fatal("review failure wrote ownership")
	}
	if err := ApplyDSHPermissions(plan, plan.ID); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(plan.Configuration)
	if !bytes.Contains(data, []byte("name: keep")) {
		t.Fatal("unrelated config lost")
	}
	roots, err := dshPermissionRoots(data)
	if err != nil || !slices.Equal(roots, []string{input.StateDir}) {
		t.Fatal(roots, err)
	}
	if err := ApplyDSHPermissions(plan, plan.ID); err == nil {
		t.Fatal("stale plan replay overwrote committed ownership")
	}
	input.EffectiveRoots = []string{input.StateDir}
	repeated, err := PlanDSHPermissions(input)
	if err != nil || repeated.Changes || repeated.OwnershipChanges || repeated.Status != "already_configured" {
		t.Fatal("same scope upgrade changed permissions", err)
	}
	if err := ApplyDSHPermissions(repeated, repeated.ID); err != nil {
		t.Fatal(err)
	}
	input.Operation = "remove"
	remove, err := PlanDSHPermissions(input)
	if err != nil || !remove.Changes {
		t.Fatal(err)
	}
	if err := ApplyDSHPermissions(remove, remove.ID); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(plan.Configuration)
	roots, err = dshPermissionRoots(data)
	if err != nil || len(roots) != 0 || !bytes.Contains(data, []byte("name: keep")) {
		t.Fatal("removal damaged other config", err)
	}
}

func TestDSHPermissionsPreserveExistingRootsAndSharedEnvironmentOwners(t *testing.T) {
	input := dshPermissionsFixture(t)
	other := filepath.Join(filepath.Dir(input.StateDir), "other")
	if err := os.MkdirAll(other, 0700); err != nil {
		t.Fatal(err)
	}
	input.EffectiveRoots = []string{other}
	first, err := PlanDSHPermissions(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyDSHPermissions(first, first.ID); err != nil {
		t.Fatal(err)
	}
	input.EffectiveRoots = []string{other, input.StateDir}
	input.Executable = filepath.Join(filepath.Dir(input.Executable), "prod", "remote-agent-session")
	input.Environment = "prod"
	second, err := PlanDSHPermissions(input)
	if err != nil || second.Changes || !second.OwnershipChanges || second.Status != "review_required" {
		t.Fatal("shared directory unexpectedly changed", err)
	}
	if err := ApplyDSHPermissions(second, second.ID); err != nil {
		t.Fatal(err)
	}
	input.Operation = "remove"
	removeSecond, err := PlanDSHPermissions(input)
	if err != nil || removeSecond.Changes {
		t.Fatal("removed another installation grant", err)
	}
	if err := ApplyDSHPermissions(removeSecond, removeSecond.ID); err != nil {
		t.Fatal(err)
	}
	input.Executable = first.Executable
	input.Environment = "dev"
	removeFirst, err := PlanDSHPermissions(input)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(removeFirst.AfterRoots, []string{other}) {
		t.Fatal("unrelated effective root lost", removeFirst.AfterRoots)
	}
	if err := ApplyDSHPermissions(removeFirst, removeFirst.ID); err != nil {
		t.Fatal(err)
	}
}

func TestDSHPermissionsNeverOwnOrRemovePreexistingUserRoot(t *testing.T) {
	input := dshPermissionsFixture(t)
	if err := os.MkdirAll(input.StateDir, 0700); err != nil {
		t.Fatal(err)
	}
	input.EffectiveRoots = []string{input.StateDir}
	first, err := PlanDSHPermissions(input)
	if err != nil || first.Changes || !first.OwnershipChanges || first.Status != "review_required" {
		t.Fatal(err)
	}
	if err := ApplyDSHPermissions(first, first.ID); err != nil {
		t.Fatal(err)
	}
	input.Operation = "remove"
	remove, err := PlanDSHPermissions(input)
	if err != nil || remove.Changes || !slices.Equal(remove.AfterRoots, input.EffectiveRoots) {
		t.Fatal("preexisting grant removed", err)
	}
	if err := ApplyDSHPermissions(remove, remove.ID); err != nil {
		t.Fatal(err)
	}
}

func TestDSHPermissionsRejectUnknownConfigurationAndSymlinkScopes(t *testing.T) {
	for _, patch := range []string{
		"unknown: mapping\n",
		"- id: sandbox-policy\n  config:\n    additionalWritableRoots: !!js process.env.ROOTS\n",
		"- id: sandbox-policy\n  config: !!js process.env.CONFIG\n",
		"- id: sandbox-policy\n  name: other-provider\n",
		"- id: sandbox-policy\n  config:\n    additionalWritableRoots: &roots []\n",
		"- id: sandbox-policy\n  config:\n    additionalWritableRoots: []\n    additionalWritableRoots: []\n",
	} {
		t.Run(patch, func(t *testing.T) {
			input := dshPermissionsFixture(t)
			file := filepath.Join(input.ProfileDirectory, "cordis.patch.yml")
			os.WriteFile(file, []byte(patch), 0600)
			if _, err := PlanDSHPermissions(input); err == nil {
				t.Fatal("unknown config accepted")
			}
			after, _ := os.ReadFile(file)
			if string(after) != patch {
				t.Fatal("refused config changed")
			}
		})
	}
	for _, kind := range []string{"state", "patch", "home-override", "profile-mismatch", "missing-policy"} {
		t.Run(kind, func(t *testing.T) {
			input := dshPermissionsFixture(t)
			switch kind {
			case "state":
				os.MkdirAll(input.StateDir+"-real", 0700)
				os.Symlink(input.StateDir+"-real", input.StateDir)
			case "patch":
				path := filepath.Join(input.ProfileDirectory, "cordis.patch.yml")
				os.Rename(path, path+"-real")
				os.Symlink(path+"-real", path)
			case "home-override":
				os.WriteFile(filepath.Join(input.Home, "cordis.patch.yml"), []byte("- id: sandbox-policy\n  config:\n    additionalWritableRoots: []\n"), 0600)
			case "profile-mismatch":
				input.Profile = "other"
			case "missing-policy":
				input.EffectiveRoots = nil
			}
			if _, err := PlanDSHPermissions(input); err == nil {
				t.Fatal("unsafe scope accepted")
			}
		})
	}
}

func TestDSHPermissionsRecoverRecordFailureWithoutRepeatingOrOverwritingConfig(t *testing.T) {
	input := dshPermissionsFixture(t)
	plan, err := PlanDSHPermissions(input)
	if err != nil {
		t.Fatal(err)
	}
	err = applyDSHPermissionFiles(plan, func(path string, data []byte, mode os.FileMode) error {
		if strings.HasSuffix(path, dshPermissionRecordName) {
			return errors.New("synthetic record failure")
		}
		return atomic(path, data, mode)
	})
	if err == nil {
		t.Fatal("record failure hidden")
	}
	input.EffectiveRoots = []string{input.StateDir}
	recovery, err := PlanDSHPermissions(input)
	if err != nil || recovery.ID != plan.ID || recovery.Status != "recovery_required" {
		t.Fatal("original review not recoverable", err)
	}
	if err := ApplyDSHPermissions(recovery, plan.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(input.ProfileDirectory, dshPermissionTransactionName)); !os.IsNotExist(err) {
		t.Fatal("completed transaction retained")
	}
}

func TestDSHPermissionsRefuseConfigChangedAfterReview(t *testing.T) {
	input := dshPermissionsFixture(t)
	plan, err := PlanDSHPermissions(input)
	if err != nil {
		t.Fatal(err)
	}
	changed := []byte("- id: other\n  config:\n    name: changed-by-user\n")
	os.WriteFile(plan.Configuration, changed, 0600)
	if err := ApplyDSHPermissions(plan, plan.ID); err == nil {
		t.Fatal("stale review overwrote new config")
	}
	actual, _ := os.ReadFile(plan.Configuration)
	if !bytes.Equal(actual, changed) {
		t.Fatal("user config overwritten")
	}
}

func TestDSHPermissionsRefuseStaleOrDynamicallyTaggedProfilePolicy(t *testing.T) {
	for _, kind := range []string{"stale-effective", "extra-effective", "tagged-config"} {
		t.Run(kind, func(t *testing.T) {
			input := dshPermissionsFixture(t)
			root := filepath.Join(filepath.Dir(input.StateDir), "other")
			os.MkdirAll(root, 0700)
			patch := filepath.Join(input.ProfileDirectory, "cordis.patch.yml")
			data := []byte("- id: sandbox-policy\n  config:\n    additionalWritableRoots:\n      - " + root + "\n")
			if kind == "extra-effective" {
				input.EffectiveRoots = []string{root, filepath.Dir(root)}
			}
			if kind == "tagged-config" {
				data = []byte("- id: sandbox-policy\n  config: !dynamic\n    additionalWritableRoots: []\n")
			}
			os.WriteFile(patch, data, 0600)
			if _, err := PlanDSHPermissions(input); err == nil {
				t.Fatal("unknown or stale effective policy accepted")
			}
			actual, _ := os.ReadFile(patch)
			if !bytes.Equal(actual, data) {
				t.Fatal("refused policy changed")
			}
		})
	}
}

func TestDSHPermissionsReviewSurvivesSameScopeAndRefusesHomeDrift(t *testing.T) {
	input := dshPermissionsFixture(t)
	first, err := PlanDSHPermissions(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := PlanDSHPermissions(input)
	if err != nil || second.ID != first.ID {
		t.Fatal("unchanged plan requires a different review", err)
	}
	os.WriteFile(filepath.Join(input.Home, "cordis.patch.yml"), []byte("- id: other\n  config:\n    changed: true\n"), 0600)
	if err := ApplyDSHPermissions(first, first.ID); err == nil {
		t.Fatal("home config drift accepted")
	}
}

func TestDSHPermissionsWriterRespectsHostProfileLock(t *testing.T) {
	input := dshPermissionsFixture(t)
	plan, err := PlanDSHPermissions(input)
	if err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(input.ProfileDirectory, "package.json.lock")
	if err := os.WriteFile(lock, []byte(strconv.Itoa(os.Getpid())+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ApplyDSHPermissions(plan, plan.ID); err == nil {
		t.Fatal("concurrent host profile writer ignored")
	}
	if _, err := os.Stat(lock); err != nil {
		t.Fatal("host lock removed")
	}
	data, _ := os.ReadFile(plan.Configuration)
	if bytes.Contains(data, []byte(input.StateDir)) {
		t.Fatal("locked profile changed")
	}
}

func TestDSHPermissionsRejectServiceStateOverlapAndAliases(t *testing.T) {
	input := dshPermissionsFixture(t)
	service := filepath.Join(filepath.Dir(input.StateDir), "service")
	if err := os.Mkdir(service, 0700); err != nil {
		t.Fatal(err)
	}
	input.ServiceStateDir = service
	for _, state := range []string{service, filepath.Dir(service), filepath.Join(service, "nested")} {
		input.StateDir = state
		if _, err := PlanDSHPermissions(input); err == nil {
			t.Fatal("overlapping service state was offered for approval", state)
		}
	}
	alias := filepath.Join(filepath.Dir(service), "service-alias")
	if err := os.Symlink(service, alias); err != nil {
		t.Fatal(err)
	}
	input.StateDir = service
	input.ServiceStateDir = alias
	if _, err := PlanDSHPermissions(input); err == nil {
		t.Fatal("service alias bypassed overlap guard")
	}
	input.StateDir = filepath.Join(filepath.Dir(service), "cli")
	input.ServiceStateDir = service
	if _, err := PlanDSHPermissions(input); err != nil {
		t.Fatal("independent CLI sibling rejected", err)
	}
	if err := os.Mkdir(input.StateDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(input.StateDir, "encryption.key"), []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := PlanDSHPermissions(input); err == nil {
		t.Fatal("service marker inside CLI root was offered")
	}
}

func TestDSHPermissionsProtectHistoricalCustomServiceRootsWithEnvironmentUnset(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("REMOTE_AGENT_SESSION_SERVICE_STATE_DIR", "")
	input := dshPermissionsFixture(t)
	plan, err := PlanDSHPermissions(input)
	if err != nil {
		t.Fatal(err)
	}
	root, err := serviceRegistryRoot()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("read-only planning created registry")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	known := filepath.Join(input.StateDir, "private-service")
	data := []byte(`{"schema":1,"roots":[` + strconv.Quote(known) + `]}`)
	if err := os.WriteFile(filepath.Join(root, "service-roots.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := PlanDSHPermissions(input); err == nil {
		t.Fatal("historical nested custom service was offered with service env unset")
	}
	if err := ApplyDSHPermissions(plan, plan.ID); err == nil {
		t.Fatal("old reviewed plan bypassed newly registered service")
	}
	for _, state := range []string{known, filepath.Join(known, "child"), filepath.Dir(known)} {
		input.StateDir = state
		if _, err := PlanDSHPermissions(input); err == nil {
			t.Fatal("registered overlap was offered", state)
		}
	}
	input.StateDir = filepath.Join(filepath.Dir(input.StateDir), "independent-client")
	if _, err := PlanDSHPermissions(input); err != nil {
		t.Fatal("independent root rejected", err)
	}
	if err := os.WriteFile(filepath.Join(root, "service-roots.json"), []byte(`{"schema":99,"roots":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := PlanDSHPermissions(input); err == nil {
		t.Fatal("malformed registry ignored")
	}
}

func TestDSHPermissionsRetainCanonicalClientRegistryAfterGrantRemoval(t *testing.T) {
	input := dshPermissionsFixture(t)
	plan, err := PlanDSHPermissions(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyDSHPermissions(plan, plan.ID); err != nil {
		t.Fatal(err)
	}
	root, err := serviceRegistryRoot()
	if err != nil {
		t.Fatal(err)
	}
	roots, err := readDSHRootRegistry(root, "client-roots.json")
	if err != nil || !slices.Contains(roots, plan.StateDir) {
		t.Fatal("successful grant did not protect custom CLI root", roots, err)
	}
	input.Operation = "remove"
	input.EffectiveRoots = []string{plan.StateDir}
	remove, err := PlanDSHPermissions(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyDSHPermissions(remove, remove.ID); err != nil {
		t.Fatal(err)
	}
	roots, err = readDSHRootRegistry(root, "client-roots.json")
	if err != nil || !slices.Contains(roots, plan.StateDir) {
		t.Fatal("removing a grant forgot retained client credentials", roots, err)
	}
}
