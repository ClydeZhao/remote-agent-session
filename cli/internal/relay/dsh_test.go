package relay

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"testing"
	"time"
)

func TestLiveDSHOriginalSessionHistory(t *testing.T) {
	socketPath := os.Getenv("REMOTE_AGENT_SESSION_TEST_DSH_SOCKET")
	instance := os.Getenv("REMOTE_AGENT_SESSION_TEST_DSH_INSTANCE")
	sessionID := os.Getenv("REMOTE_AGENT_SESSION_TEST_DSH_SESSION")
	if socketPath == "" || instance == "" || sessionID == "" {
		t.Skip("isolated DSH session not selected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	d, err := OpenDSH(ctx, socketPath, sessionID, instance)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.CheckHistory(ctx, sessionID); err != nil {
		t.Fatal(err)
	}
	id := os.Getenv("REMOTE_AGENT_SESSION_TEST_DSH_REQUEST")
	if id == "" {
		return
	}
	outcome, err := d.Outcome(ctx, sessionID, id, time.Time{})
	if err != nil || !outcome.Found || !outcome.Complete || outcome.Status != "completed" || outcome.Mixed || outcome.Text == "" {
		t.Fatalf("unexpected outcome found=%t complete=%t status=%s mixed=%t error=%v", outcome.Found, outcome.Complete, outcome.Status, outcome.Mixed, err)
	}
}

func TestLiveDSHDiscovery(t *testing.T) {
	sessionID := os.Getenv("REMOTE_AGENT_SESSION_TEST_DSH_SESSION")
	if sessionID == "" {
		t.Skip("isolated DSH session not selected")
	}
	directory, err := DSHDirectory()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, runtime, err := DiscoverDSH(ctx, directory, sessionID, "")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if runtime == "" {
		t.Fatal("missing stable runtime identity")
	}
	if err := client.CheckHistory(ctx, sessionID); err != nil {
		t.Fatal(err)
	}
}

func dshTestClient(t *testing.T, handle func(string, map[string]any) any) *DSH {
	t.Helper()
	client, server := net.Pipe()
	d := &DSH{rpc: NewRPC(client, client, func() { client.Close() }), target: DSHTarget{Instance: "instance", Owner: "owner", SessionID: "session-test", Cwd: "/test/workspace"}}
	t.Cleanup(d.Close)
	go func() {
		defer server.Close()
		scanner := bufio.NewScanner(server)
		for scanner.Scan() {
			var request struct {
				ID     int64          `json:"id"`
				Method string         `json:"method"`
				Params map[string]any `json:"params"`
			}
			if json.Unmarshal(scanner.Bytes(), &request) != nil {
				return
			}
			result := handle(request.Method, request.Params)
			if json.NewEncoder(server).Encode(map[string]any{"id": request.ID, "result": result}) != nil {
				return
			}
		}
	}()
	return d
}

func TestDSHInspectReportsDSHSessionName(t *testing.T) {
	d := dshTestClient(t, func(method string, params map[string]any) any {
		if method != "inspect" || params["session_id"] != "session-test" {
			t.Error("unexpected target inspection", method, params)
		}
		return DSHTarget{Instance: "instance", Owner: "owner", SessionID: "session-test", Cwd: "/test/workspace", Status: "idle"}
	})
	thread, err := d.InspectRelayTarget(context.Background(), "session-test")
	if err != nil || thread.Name != "DSH 会话" || thread.ID != "session-test" || thread.Cwd != "/test/workspace" {
		t.Fatalf("unexpected target: %+v %v", thread, err)
	}
}

func TestDSHRefusesInputWhenOwnerChanges(t *testing.T) {
	d := dshTestClient(t, func(method string, params map[string]any) any {
		if method != "inspect" {
			t.Error("sent mutation after owner changed", method)
		}
		return DSHTarget{Instance: "instance", Owner: "replacement", SessionID: "session-test", Cwd: "/test/workspace", Status: "idle"}
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := d.StartRemoteInput(ctx, "session-test", "request", "hello", func() error { t.Error("authorized replaced owner"); return nil })
	if !errors.Is(err, ErrRemoteInputNotSubmitted) {
		t.Fatal(err)
	}
}

func TestDSHReadinessRequiresAllUsedInteractionMethods(t *testing.T) {
	for _, test := range []struct {
		name         string
		capabilities map[string]any
		valid        bool
	}{
		{"old plugin", map[string]any{"protocol": 1, "interactions": true}, false},
		{"missing sync", map[string]any{"protocol": 1, "continued": true}, false},
		{"unknown protocol", map[string]any{"protocol": 2, "interactions": true, "continued": true}, false},
		{"current", map[string]any{"protocol": 1, "interactions": true, "continued": true}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := dshTestClient(t, func(method string, params map[string]any) any {
				if method != "capabilities" {
					t.Error("unexpected mutation during readiness", method)
				}
				return test.capabilities
			})
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := d.CheckInteractions(ctx); (err == nil) != test.valid {
				t.Fatalf("readiness err=%v valid=%t", err, test.valid)
			}
		})
	}
}

func TestDSHRechecksAuthorizationBeforeSubmitting(t *testing.T) {
	d := dshTestClient(t, func(method string, params map[string]any) any {
		if method != "inspect" {
			t.Error("sent mutation after authorization expired", method)
		}
		return DSHTarget{Instance: "instance", Owner: "owner", SessionID: "session-test", Cwd: "/test/workspace", Status: "idle"}
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := d.StartRemoteInput(ctx, "session-test", "request", "hello", func() error { return errors.New("expired") })
	if !errors.Is(err, ErrRemoteInputNotSubmitted) {
		t.Fatal(err)
	}
}

func TestDSHSubmissionCarriesPinnedIdentityAndStableRequest(t *testing.T) {
	d := dshTestClient(t, func(method string, params map[string]any) any {
		if method == "inspect" {
			return DSHTarget{Instance: "instance", Owner: "owner", SessionID: "session-test", Cwd: "/test/workspace", Status: "idle"}
		}
		if method != "submit" || params["instance"] != "instance" || params["owner"] != "owner" || params["session_id"] != "session-test" || params["cwd"] != "/test/workspace" || params["request_id"] != "request" || params["text"] != "hello" {
			t.Error("incorrect dispatch", method, params)
		}
		return map[string]bool{"accepted": true}
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := d.StartRemoteInput(ctx, "session-test", "request", "hello", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestDSHPermissionPolicyRequiresExplicitRootsAndNormalSandbox(t *testing.T) {
	for _, test := range []struct {
		name   string
		policy DSHPermissionPolicy
		valid  bool
	}{
		{"normal", DSHPermissionPolicy{Mode: "workspace-write", Profile: "desktop", ProfileDirectory: "/profile/desktop", WorkspaceRoot: "/workspace", AdditionalWritableRoots: []string{}}, true},
		{"missing roots", DSHPermissionPolicy{Mode: "workspace-write", Profile: "desktop", ProfileDirectory: "/profile/desktop", WorkspaceRoot: "/workspace"}, false},
		{"full access", DSHPermissionPolicy{Mode: "danger-full-access", Profile: "desktop", ProfileDirectory: "/profile/desktop", WorkspaceRoot: "/workspace", AdditionalWritableRoots: []string{}}, false},
		{"relative root", DSHPermissionPolicy{Mode: "workspace-write", Profile: "desktop", ProfileDirectory: "/profile/desktop", WorkspaceRoot: "/workspace", AdditionalWritableRoots: []string{"relative"}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := dshTestClient(t, func(method string, params map[string]any) any {
				if method != "permission_policy" || params["owner"] != "owner" || params["session_id"] != "session-test" {
					t.Error("policy request lost original identity", method)
				}
				return test.policy
			})
			_, err := d.PermissionPolicy(context.Background(), "session-test")
			if (err == nil) != test.valid {
				t.Fatal("incorrect policy admission", err)
			}
		})
	}
}

func TestDSHSummaryUsesOnlyItsExactNativeOwner(t *testing.T) {
	d := dshTestClient(t, func(method string, params map[string]any) any {
		if method != "summarize" || params["session_id"] != "session-test" || params["instance"] != "instance" || params["owner"] != "owner" || params["text"] != "材料" {
			t.Error("summary escaped exact DSH target")
		}
		return map[string]any{"summary": "结论尚未确认，无需回复。"}
	})
	result, err := Summarize(context.Background(), d, "材料")
	if err != nil || result != "结论尚未确认，无需回复。" {
		t.Fatal(result, err)
	}
	if _, err := Summarize(context.Background(), nil, "材料"); err == nil {
		t.Fatal("unknown host fell back to Codex")
	}
}
