package app

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("REMOTE_AGENT_SESSION_TEST_HOOK_ARGS") == "1" && len(os.Args) > 1 && os.Args[1] == "--state-dir" {
		input, err := io.ReadAll(io.LimitReader(os.Stdin, 4096))
		if err != nil {
			os.Exit(2)
		}
		if json.NewEncoder(os.Stdout).Encode(map[string]any{"args": os.Args[1:], "input": string(input)}) != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestWindowsHookCommandQuotesPaths(t *testing.T) {
	command, err := formatRelayHookCommand(`C:\Users\Test & Co\agent.exe`, `C:\state dir\`)
	if err != nil {
		t.Fatal(err)
	}
	if command != `"C:\Users\Test & Co\agent.exe" --state-dir "C:\state dir\\" session hook` {
		t.Fatalf("unexpected command %q", command)
	}
	for _, unsafe := range []string{"percent%PATH%", "delayed!PATH!", "line\nnext", "quote\"", "nul\x00"} {
		if _, err := formatRelayHookCommand(`C:\agent.exe`, unsafe); err == nil {
			t.Fatalf("accepted cmd expansion/control characters %q", unsafe)
		}
	}
}

func TestWindowsHookCommandExecutesThroughCodexCmdRunner(t *testing.T) {
	original, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "hook tools & fixtures", "relay helper.exe")
	if err := os.MkdirAll(filepath.Dir(binary), 0700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(original)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, data, 0700); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(t.TempDir(), "state path & fixtures") + `\`
	command, err := formatRelayHookCommand(binary, state)
	if err != nil {
		t.Fatal(err)
	}
	shell := os.Getenv("COMSPEC")
	if shell == "" {
		shell = filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: syscall.EscapeArg(shell) + ` /C "` + command + `"`}
	cmd.Env = append(os.Environ(), "REMOTE_AGENT_SESSION_TEST_HOOK_ARGS=1")
	input := `{"hook_event_name":"PermissionRequest","session_id":"fixture"}`
	cmd.Stdin = strings.NewReader(input)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Codex-compatible cmd runner failed: %v; %s", err, output)
	}
	var actual struct {
		Args  []string `json:"args"`
		Input string   `json:"input"`
	}
	if err := json.Unmarshal(output, &actual); err != nil {
		t.Fatal(err)
	}
	expected := []string{"--state-dir", state, "session", "hook"}
	if !reflect.DeepEqual(actual.Args, expected) || actual.Input != input {
		t.Fatalf("hook boundary changed arguments/input: %+v", actual)
	}
}
