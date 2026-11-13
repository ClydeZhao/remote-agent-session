package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginCheckWithoutSessionDoesNotRequireLoginOrCreateState(t *testing.T) {
	t.Setenv("DSH_SESSION_ID", "")
	dir := filepath.Join(t.TempDir(), "unconfigured")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--json", "--state-dir", dir, "session", "plugin", "check", "--agent", "deepseek-harness"}, &stdout, &stderr)
	if code != exitIncomplete {
		t.Fatalf("unexpected exit %d: %s %s", code, stdout.String(), stderr.String())
	}
	var result struct {
		Data struct {
			Local  bool         `json:"local_runtime_available"`
			Feishu bool         `json:"feishu_checked"`
			Checks []diagnostic `json:"checks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.Local || result.Data.Feishu {
		t.Fatal("unverified readiness claimed")
	}
	found := false
	stateAccessExplained := false
	for _, check := range result.Data.Checks {
		if check.Name == "original_session" {
			found = check.Status == "not_checked" && check.CheckedAt == nil
		}
		if check.Name == "local_state_access" {
			stateAccessExplained = check.Status == "not_checked" && check.CheckedAt == nil && strings.Contains(check.Message, "插件启用") && strings.Contains(check.Message, "单次审批")
		}
	}
	if !found {
		t.Fatal("missing native session not explained")
	}
	if !stateAccessExplained {
		t.Fatal("plugin check conflates plugin availability with persistent local state access")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("read-only diagnostic created login state")
	}
}
