//go:build !windows

package relay

import (
	"context"
	"encoding/json"
	"github.com/coder/websocket"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultOwnerDiscoveryIgnoresCreationClient(t *testing.T) {
	for _, scenario := range []struct {
		name, id, cwd, status string
		accepted              bool
		explicit              bool
	}{
		{"desktop_created", "thread", "/test", "idle", true, false},
		{"wrong_thread", "other", "/test", "idle", false, false},
		{"wrong_project", "thread", "/other", "idle", false, false},
		{"unloaded", "thread", "/test", "notLoaded", false, false},
		{"explicit_loaded", "thread", "/test", "idle", true, true},
		{"explicit_unloaded", "thread", "/test", "notLoaded", false, true},
		{"explicit_missing_status", "thread", "/test", "", false, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			dir, err := os.MkdirTemp("/tmp", "bb-owner-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			socket := filepath.Join(dir, "owner.sock")
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer conn.CloseNow()
				for {
					_, data, err := conn.Read(r.Context())
					if err != nil {
						return
					}
					var request struct {
						ID     int64  `json:"id"`
						Method string `json:"method"`
					}
					if json.Unmarshal(data, &request) != nil {
						return
					}
					if request.ID == 0 {
						continue
					}
					result := map[string]any{}
					if request.Method == "thread/read" {
						result["thread"] = map[string]any{"id": scenario.id, "cwd": scenario.cwd, "originator": "codex-desktop", "status": map[string]string{"type": scenario.status}}
					}
					response, _ := json.Marshal(map[string]any{"id": request.ID, "result": result})
					if conn.Write(r.Context(), websocket.MessageText, response) != nil {
						return
					}
				}
			})}
			go server.Serve(listener)
			defer server.Close()
			binary := filepath.Join(dir, "codex")
			program := `#!/usr/bin/env python3
import sys,json
if sys.argv[1:]==['app-server','daemon','version']:
 print(json.dumps({'status':'running','socketPath':sys.argv[0]+'.sock'}))
else:
 for line in sys.stdin:
  request=json.loads(line)
  if 'id' not in request: continue
  result={}
  if request['method']=='thread/read': result={'thread':{'id':'thread','cwd':'/test','originator':'codex-desktop','status':{'type':'notLoaded'}}}
  print(json.dumps({'id':request['id'],'result':result}),flush=True)
`
			if err := os.WriteFile(binary, []byte(program), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(socket, binary+".sock"); err != nil {
				t.Fatal(err)
			}
			t.Setenv("REMOTE_AGENT_SESSION_CODEX_SOCKET", "")
			if scenario.explicit {
				t.Setenv("REMOTE_AGENT_SESSION_CODEX_SOCKET", socket)
			}
			t.Setenv("CODEX_HOME", dir)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			c, err := OpenCodexForThread(ctx, binary, "thread")
			if !scenario.accepted {
				if err == nil {
					c.RPC.Close()
					t.Fatal("accepted unavailable or mismatched executor")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer c.RPC.Close()
			thread, err := c.Inspect(ctx, "thread")
			if err != nil {
				t.Fatal(err)
			}
			if (thread.Status.Type == "idle") != scenario.accepted {
				t.Fatalf("owner acceptance=%v, thread=%+v", scenario.accepted, thread)
			}
		})
	}
}
