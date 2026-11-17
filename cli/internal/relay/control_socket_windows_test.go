package relay

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sys/windows"
)

func TestWindowsControlSocketACL(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid := user.User.Sid.String()
	for _, test := range []struct {
		name, descriptor string
		private, safe    bool
	}{
		{"private", "O:" + sid + "D:P(A;;FA;;;" + sid + ")(A;;FA;;;SY)(A;;FA;;;BA)", true, true},
		{"other_owner", "O:WDD:P(A;;FA;;;" + sid + ")", true, false},
		{"null_dacl", "O:" + sid + "D:NO_ACCESS_CONTROL", true, false},
		{"public_read", "O:" + sid + "D:P(A;;FR;;;WD)(A;;FA;;;" + sid + ")", true, false},
		{"ancestor_public_read", "O:" + sid + "D:P(A;;FR;;;WD)(A;;FA;;;" + sid + ")", false, true},
		{"ancestor_public_write", "O:" + sid + "D:P(A;;GW;;;WD)(A;;FA;;;" + sid + ")", false, false},
		{"ancestor_public_delete_child", "O:" + sid + "D:P(A;;0x40;;;WD)(A;;FA;;;" + sid + ")", false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			sd, err := windows.SecurityDescriptorFromString(test.descriptor)
			if err != nil {
				t.Fatal(err)
			}
			if safe := controlSocketSecuritySafe(sd, user.User.Sid, test.private); safe != test.safe {
				t.Fatalf("safe=%v, expected %v", safe, test.safe)
			}
		})
	}
}

func TestWindowsCodexRequiresExplicitBackend(t *testing.T) {
	t.Setenv("REMOTE_AGENT_SESSION_CODEX_SOCKET", "")
	if c, err := OpenCodexForThread(context.Background(), "must-not-start", "thread"); err == nil {
		c.RPC.Close()
		t.Fatal("accepted a Windows session without its explicit original backend")
	}
	for _, name := range []string{"relative.sock", `\\server\share\control.sock`, `C:\home\socket:stream`} {
		if _, _, err := prepareControlSocket(name); err == nil {
			t.Fatalf("accepted unsafe socket path %q", name)
		}
	}
	regular := filepath.Join(t.TempDir(), "regular")
	if err := os.WriteFile(regular, []byte("not a socket"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := validateControlSocket(regular); err == nil {
		t.Fatal("accepted a regular file as control socket")
	}
}

func TestLiveWindowsOriginalRelay(t *testing.T) {
	socket := os.Getenv("REMOTE_AGENT_SESSION_TEST_CONTROL_SOCKET")
	thread := os.Getenv("REMOTE_AGENT_SESSION_TEST_OWNED_THREAD")
	if socket == "" || thread == "" || os.Getenv("REMOTE_AGENT_SESSION_TEST_CONTROL_MUTATIONS") != "1" {
		t.Skip("explicit isolated Codex fixture, loaded original thread and mutation opt-in required")
	}
	t.Setenv("REMOTE_AGENT_SESSION_CODEX_SOCKET", socket)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	open := func() *Codex {
		t.Helper()
		client, err := OpenCodexForThread(ctx, "must-not-start-an-executor", thread)
		if err != nil {
			t.Fatal(err)
		}
		return client
	}
	client := open()
	defer func() { client.RPC.Close() }()
	if unknown, err := OpenCodexForThread(ctx, "must-not-start-an-executor", uuid.NewString()); err == nil {
		unknown.RPC.Close()
		t.Fatal("accepted unknown target")
	}
	denied := errors.New("test authorization denial")
	if err := client.StartRemoteInput(ctx, thread, uuid.NewString(), "MUST_NOT_SUBMIT", func() error { return denied }); !errors.Is(err, denied) || !errors.Is(err, ErrRemoteInputNotSubmitted) {
		t.Fatalf("authorization denial was not preserved: %v", err)
	}
	authorized := 0
	authorize := func() error { authorized++; return nil }
	await := func(id, status, text string) Outcome {
		t.Helper()
		for {
			outcome, err := client.Outcome(ctx, thread, id, time.Now().Add(-time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			if outcome.Found && outcome.Complete {
				if outcome.Status != status || outcome.Text != text || len(outcome.OriginIDs) != 1 || outcome.OriginIDs[0] != id {
					t.Fatalf("wrong original-session outcome: %+v", outcome)
				}
				return outcome
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	first := uuid.NewString()
	if err := client.StartRemoteInput(ctx, thread, first, "RAS_REMOTE", authorize); err != nil {
		t.Fatal(err)
	}
	await(first, "completed", "RAS_REMOTE")
	waiting := uuid.NewString()
	if err := client.StartRemoteInput(ctx, thread, waiting, "RAS_WAIT", authorize); err != nil {
		t.Fatal(err)
	}
	for {
		outcome, err := client.Outcome(ctx, thread, waiting, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Found && outcome.Status == "inProgress" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	if err := client.InterruptActive(ctx, thread, authorize); err != nil {
		t.Fatal(err)
	}
	await(waiting, "interrupted", "")
	client.RPC.Close()
	client = open()
	continued := uuid.NewString()
	if err := client.StartRemoteInput(ctx, thread, continued, "RAS_RECONNECT", authorize); err != nil {
		t.Fatal(err)
	}
	await(continued, "completed", "RAS_RECONNECTED")
	if authorized != 4 {
		t.Fatalf("authorization callbacks=%d, expected 4", authorized)
	}
	t.Log("production relay verified original-session source IDs, authorization, remote input, interrupt, outcome and reconnect")
}
