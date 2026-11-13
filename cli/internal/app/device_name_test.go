package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/ClydeZhao/remote-agent-session/cli/internal/api"
	"github.com/google/uuid"
)

func TestDeviceNameUsesComputerNameAndFallsBackToHostname(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		platform string
		output   string
		failure  error
		timeout  bool
		expected string
	}{
		{name: "computer name", platform: "darwin", output: "  Lab Computer\n", expected: "Lab Computer"},
		{name: "unicode computer name", platform: "darwin", output: "科研团队’s Mac\n", expected: "科研团队’s Mac"},
		{name: "empty", platform: "darwin", output: " \n", expected: "network-name"},
		{name: "failure", platform: "darwin", failure: errors.New("unavailable"), expected: "network-name"},
		{name: "timeout", platform: "darwin", timeout: true, expected: "network-name"},
		{name: "linux", platform: "linux", expected: "network-name"},
		{name: "windows", platform: "windows", expected: "network-name"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			commands, hostnames := 0, 0
			started := time.Now()
			name, err := platformDeviceName(scenario.platform, func() (string, error) {
				hostnames++
				return "network-name", nil
			}, func(ctx context.Context, command string, args ...string) ([]byte, error) {
				commands++
				if command != "/usr/sbin/scutil" || !reflect.DeepEqual(args, []string{"--get", "ComputerName"}) {
					t.Fatal("unexpected system lookup", command, args)
				}
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > 500*time.Millisecond {
					t.Fatal("system lookup has no short deadline")
				}
				if scenario.timeout {
					<-ctx.Done()
					return nil, ctx.Err()
				}
				return []byte(scenario.output), scenario.failure
			})
			if err != nil || name != scenario.expected || time.Since(started) > 2*time.Second {
				t.Fatal(name, err, time.Since(started))
			}
			if scenario.platform != "darwin" && commands != 0 || scenario.expected == "network-name" && hostnames != 1 || scenario.expected != "network-name" && hostnames != 0 {
				t.Fatal("incorrect lookup path", commands, hostnames)
			}
		})
	}
}

func TestDeviceNamePreservesHostnameFailure(t *testing.T) {
	failure := errors.New("hostname unavailable")
	_, err := platformDeviceName("linux", func() (string, error) { return "", failure }, func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("unexpected ComputerName lookup")
		return nil, nil
	})
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
}

func TestLoginUsesSystemDeviceNameAndPreservesExplicitName(t *testing.T) {
	name, err := deviceName()
	if err != nil {
		t.Fatal(err)
	}
	for _, explicit := range []string{"", "User chosen name"} {
		t.Run(explicit, func(t *testing.T) {
			expected := name
			if explicit != "" {
				expected = explicit
			}
			dir, _ := testSetup(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/v1/auth/devices" {
					t.Error("unexpected login request", r.Method, r.URL.Path)
				}
				var body api.DeviceStart
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.DeviceName != expected {
					t.Error("incorrect device name", body.DeviceName, expected, err)
				}
				jsonReply(w, 200, api.DeviceCreated{DeviceId: uuid.MustParse(testID), ClaimSecret: "synthetic-claim", UserCode: "SYNTHETIC", ExpiresAt: time.Now().Add(time.Minute)})
			})
			args := []string{"login"}
			if explicit != "" {
				args = append(args, "--device-name", explicit)
			}
			if code, result := runJSON(t, dir, args...); code != 0 {
				t.Fatal(code, result)
			}
		})
	}
}
