//go:build !windows

package relay

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func discoveryEndpoint(t *testing.T, directory, name, owner string) {
	t.Helper()
	path := filepath.Join(directory, name+".sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(DSHDescriptor{Protocol: 1, Runtime: "deepseek-harness", Instance: name, Socket: path})
	if err := os.WriteFile(filepath.Join(directory, name+".json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				scanner := bufio.NewScanner(conn)
				for scanner.Scan() {
					var request struct {
						ID int64 `json:"id"`
					}
					if json.Unmarshal(scanner.Bytes(), &request) != nil {
						return
					}
					response := map[string]any{"id": request.ID}
					if owner == "" {
						response["error"] = map[string]any{"code": -32000, "message": "owner_unavailable"}
					} else {
						response["result"] = DSHTarget{Instance: name, Owner: owner, SessionID: "session-test", Cwd: "/workspace", Status: "idle"}
					}
					if json.NewEncoder(conn).Encode(response) != nil {
						return
					}
				}
			}()
		}
	}()
}

func discoveryDirectory(t *testing.T) string {
	t.Helper()
	directory, err := os.MkdirTemp("/tmp", "bb-dsh-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(directory) })
	return directory
}

func TestDSHDiscoverySelectsExactOwnerAndStableRuntime(t *testing.T) {
	directory := discoveryDirectory(t)
	discoveryEndpoint(t, directory, "other", "")
	discoveryEndpoint(t, directory, "actual", "owner")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, runtime, err := DiscoverDSH(ctx, directory, "session-test", "")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	expected, err := DSHRuntimeIdentity(directory)
	if err != nil || runtime != expected || client.target.Owner != "owner" {
		t.Fatal("wrong runtime target")
	}
	if _, _, err := DiscoverDSH(ctx, directory, "session-test", "different-profile"); err == nil {
		t.Fatal("accepted wrong profile")
	}
}

func TestDSHDiscoveryRejectsAmbiguousOwners(t *testing.T) {
	directory := discoveryDirectory(t)
	discoveryEndpoint(t, directory, "first", "first-owner")
	discoveryEndpoint(t, directory, "second", "second-owner")
	if client, _, err := DiscoverDSH(context.Background(), directory, "session-test", ""); err == nil || client != nil {
		t.Fatal("accepted ambiguous owner")
	}
}

func TestDSHDiscoveryRejectsPublicOrSymlinkedDescriptors(t *testing.T) {
	for _, mode := range []string{"public", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			directory := discoveryDirectory(t)
			discoveryEndpoint(t, directory, "actual", "owner")
			path := filepath.Join(directory, "actual.json")
			if mode == "public" {
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			} else {
				real := filepath.Join(directory, "saved")
				if err := os.Rename(path, real); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(real, path); err != nil {
					t.Fatal(err)
				}
			}
			if client, _, err := DiscoverDSH(context.Background(), directory, "session-test", ""); err == nil || client != nil {
				t.Fatal("accepted unsafe descriptor")
			}
		})
	}
}
