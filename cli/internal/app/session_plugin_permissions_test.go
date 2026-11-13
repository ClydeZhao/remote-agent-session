package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/ClydeZhao/remote-agent-session/cli/internal/relay"
)

func TestDSHPluginPermissionPlanAndApplyUseOriginalPolicyWithoutOpeningBusinessState(t *testing.T) {
	for _, preexisting := range []bool{false, true} {
		name := "new-root"
		if preexisting {
			name = "user-root-without-owner"
		}
		t.Run(name, func(t *testing.T) { runDSHPluginPermissionPlanAndApply(t, preexisting) })
	}
}

func runDSHPluginPermissionPlanAndApply(t *testing.T, preexisting bool) {
	temporary, err := os.MkdirTemp("/tmp", "bb-dsh-perm-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(temporary) })
	root, err := filepath.EvalSymlinks(temporary)
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
	directory := filepath.Join(home, "remote-agent-session")
	state := filepath.Join(root, "private-state")
	var configured atomic.Bool
	configured.Store(preexisting)
	for _, path := range []string{profile, directory} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if preexisting {
		if err := os.Mkdir(state, 0700); err != nil {
			t.Fatal(err)
		}
	}
	patch := filepath.Join(profile, "cordis.patch.yml")
	if err := os.WriteFile(patch, []byte("[]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(directory, "bridge.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	os.Chmod(socket, 0600)
	descriptor, _ := json.Marshal(relay.DSHDescriptor{Protocol: 1, Runtime: "deepseek-harness", Instance: "permission-instance", Socket: socket})
	if err := os.WriteFile(filepath.Join(directory, "instance.json"), descriptor, 0600); err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer connection.Close()
				scanner := bufio.NewScanner(connection)
				for scanner.Scan() {
					var request struct {
						ID     int64          `json:"id"`
						Method string         `json:"method"`
						Params map[string]any `json:"params"`
					}
					if json.Unmarshal(scanner.Bytes(), &request) != nil {
						return
					}
					var result any
					switch request.Method {
					case "inspect":
						result = relay.DSHTarget{Instance: "permission-instance", Owner: "permission-owner", SessionID: "session-permissions", Cwd: root, Status: "idle"}
					case "permission_policy":
						if request.Params["owner"] != "permission-owner" || request.Params["session_id"] != "session-permissions" {
							t.Error("permission policy queried without exact identity")
						}
						roots := []string{}
						if configured.Load() {
							roots = append(roots, state)
						}
						result = relay.DSHPermissionPolicy{Mode: "workspace-write", Profile: "desktop", ProfileDirectory: profile, WorkspaceRoot: root, AdditionalWritableRoots: roots}
					default:
						t.Error("permission plan accessed unrelated native method", request.Method)
						return
					}
					if json.NewEncoder(connection).Encode(map[string]any{"id": request.ID, "result": result}) != nil {
						return
					}
				}
			}()
		}
	}()
	t.Setenv("DSH_HOME", home)
	t.Setenv("REMOTE_AGENT_SESSION_ENV_HOME", filepath.Join(root, "unused-environment"))
	args := []string{"--json", "--state-dir", state, "session", "plugin", "install", "--agent", "deepseek-harness", "--profile", "desktop", "--session", "session-permissions", "--permissions"}
	var stdout, stderr bytes.Buffer
	if code := Run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("plan failed %d %s %s", code, stdout.String(), stderr.String())
	}
	var response struct {
		Data struct {
			Plan struct {
				ID        string `json:"plan_id"`
				Profile   string `json:"profile"`
				State     string `json:"state_directory"`
				Status    string `json:"status"`
				Changes   bool   `json:"configuration_changes"`
				Ownership bool   `json:"ownership_changes"`
			}
			Command  string `json:"apply_command"`
			Applied  bool   `json:"configuration_applied"`
			Verified bool   `json:"runtime_access_verified"`
		}
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.Plan.ID == "" || response.Data.Plan.Profile != "desktop" || response.Data.Plan.State != state || response.Data.Applied || response.Data.Verified || response.Data.Command == "" {
		t.Fatal("plan lacks reviewable scope", stdout.String())
	}
	if response.Data.Plan.Status != "review_required" || !response.Data.Plan.Ownership || response.Data.Plan.Changes == preexisting {
		t.Fatal("first ownership registration skipped review", stdout.String())
	}
	if _, err := os.Stat(state); !preexisting && !os.IsNotExist(err) {
		t.Fatal("plan created business state")
	}
	var humanOut, humanErr bytes.Buffer
	if code := Run(args[1:], &humanOut, &humanErr); code != 0 || !bytes.Contains(humanOut.Bytes(), []byte(response.Data.Plan.ID)) || !bytes.Contains(humanOut.Bytes(), []byte(state)) || !bytes.Contains(humanOut.Bytes(), []byte(response.Data.Command)) {
		t.Fatal("human plan omits exact review scope or executable apply command", humanOut.String(), humanErr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run(append(args, "--apply", response.Data.Plan.ID), &stdout, &stderr); code != 0 {
		t.Fatalf("apply failed %d %s %s", code, stdout.String(), stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"configuration_applied":true`)) || !bytes.Contains(stdout.Bytes(), []byte(`"runtime_access_verified":false`)) {
		t.Fatal("save result falsely claims loaded permission", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(state, ".lock")); !os.IsNotExist(err) {
		t.Fatal("permission install opened the business state lock")
	}
	actual, _ := os.ReadFile(patch)
	if !preexisting && !bytes.Contains(actual, []byte(state)) {
		t.Fatal("reviewed root not saved")
	}
	configured.Store(true)
	recordPath := filepath.Join(profile, ".remote-agent-session-permissions.json")
	recordBefore, err := os.Stat(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run(args, &stdout, &stderr); code != 0 {
		t.Fatal("same-scope check failed", stdout.String(), stderr.String())
	}
	response.Data.Command = ""
	response.Data.Plan.Ownership = false
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.Plan.Status != "already_configured" || response.Data.Plan.Changes || response.Data.Plan.Ownership || response.Data.Command != "" || response.Data.Verified || response.Data.Applied {
		t.Fatal("same-scope upgrade repeated permission review", stdout.String())
	}
	recordAfter, err := os.Stat(recordPath)
	if err != nil || !os.SameFile(recordBefore, recordAfter) || !recordBefore.ModTime().Equal(recordAfter.ModTime()) {
		t.Fatal("read-only configured check rewrote ownership")
	}
	humanOut.Reset()
	humanErr.Reset()
	if code := Run(args[1:], &humanOut, &humanErr); code != 0 || !bytes.Contains(humanOut.Bytes(), []byte("无需重复确认或应用")) || bytes.Contains(humanOut.Bytes(), []byte("--apply")) {
		t.Fatal("same-scope human guidance still requests review", humanOut.String(), humanErr.String())
	}
}
