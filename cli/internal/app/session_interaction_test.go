package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestHookAnswersAreScopedAndFailClosed(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python 3 required for Codex boundary fixture")
	}
	resolved, resolveErr := exec.Command(python, "-c", "import sys; print(sys.executable)").Output()
	if resolveErr != nil {
		t.Fatal(resolveErr)
	}
	python = strings.TrimSpace(string(resolved))
	for _, scenario := range []string{"allow", "deny", "question", "cancelled", "mixed", "revoked", "secret", "local_permission", "local_question", "local_secret", "origin_unavailable", "inactive", "initial_mixed", "unreadable", "codex_unavailable", "remote_failure", "registration_lost", "attention", "attention_already_delivered", "attention_retry", "attention_cancelled", "local_attention", "attention_file", "local_attention_file"} {
		t.Run(scenario, func(t *testing.T) {
			bin := t.TempDir()
			t.Setenv("REMOTE_AGENT_SESSION_CODEX_SOCKET", "")
			if scenario == "codex_unavailable" {
				t.Setenv("REMOTE_AGENT_SESSION_CODEX_SOCKET", filepath.Join(bin, "absent.sock"))
			}
			fixture := "#!" + python + "\n" + `import json,sys
if sys.argv[1:]==['app-server','daemon','version']:
 print(json.dumps({'status':'stopped'}))
 sys.exit(0)
count=0
for line in sys.stdin:
 r=json.loads(line)
 if 'id' not in r:continue
 result={}
 if r['method']=='thread/read':
  result={'thread':{'id':r['params']['threadId'],'cwd':'/synthetic/work','status':{'type':'active'}}}
 if r['method']=='thread/turns/list':
  if SCENARIO=='unreadable':
   print(json.dumps({'id':r['id'],'error':{'code':-1,'message':'unavailable'}}),flush=True)
   continue
  count+=1
  items=[{'type':'userMessage','clientId':'11111111-1111-4111-8111-111111111111'}]
  if count>1 and SCENARIO=='mixed':items.append({'type':'userMessage','clientId':'local'})
  result={'data':[{'id':'turn','status':'inProgress','itemsView':'full','items':items}],'nextCursor':None}
 print(json.dumps({'id':r['id'],'result':result}),flush=True)
`
			fixture = strings.ReplaceAll(fixture, "SCENARIO", "'"+scenario+"'")
			if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(fixture), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			reads, closes := 0, 0
			registeredID := ""
			firstAttempt := ""
			reusedNotice := "22222222-2222-4222-8222-222222222222"
			dir, _ := testSetup(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/v1/session-relay":
					jsonReply(w, 200, map[string]any{"native_session": testID, "generation": 1, "desired": true, "online": true, "status": "ready"})
				case r.URL.Path == "/v1/session-relay/origin":
					if scenario == "origin_unavailable" {
						jsonReply(w, 503, map[string]string{"code": "UNAVAILABLE"})
						return
					}
					scope := "remote"
					if strings.HasPrefix(scenario, "local_") {
						scope = "local"
					}
					if scenario == "inactive" {
						scope = "inactive"
					}
					if scenario == "initial_mixed" {
						scope = "mixed"
					}
					jsonReply(w, 200, map[string]string{"scope": scope})
				case r.Method == "POST" && r.URL.Path == "/v1/session-relay/interactions":
					if strings.HasPrefix(scenario, "local_") || scenario == "origin_unavailable" || scenario == "inactive" || scenario == "initial_mixed" || scenario == "unreadable" || scenario == "codex_unavailable" {
						t.Error("nonremote interaction submitted")
						jsonReply(w, 409, map[string]string{"code": "SESSION_ORIGIN_INVALID"})
						return
					}
					if scenario == "remote_failure" {
						jsonReply(w, 503, map[string]string{"code": "UNAVAILABLE"})
						return
					}
					if scenario == "secret" {
						t.Error("secret question sent remotely")
					}
					var body map[string]any
					json.NewDecoder(r.Body).Decode(&body)
					registeredID, _ = body["id"].(string)
					if scenario == "attention_retry" {
						if firstAttempt == "" {
							firstAttempt = registeredID
							w.Write([]byte("{"))
							return
						}
						if registeredID == firstAttempt {
							t.Error("retry reused the cancelled attempt id")
						}
						registeredID = reusedNotice
					}
					if scenario == "registration_lost" {
						w.Write([]byte("{"))
						return
					}
					switch {
					case scenario == "question":
						if body["choices"].(map[string]any)["B"] != "B" || !strings.Contains(body["prompt"].(string), "**B．** B") {
							t.Error("missing question choices")
						}
					case strings.HasPrefix(scenario, "attention"):
						if body["kind"] != "attention" || strings.Contains(body["prompt"].(string), "synthetic request") {
							t.Error("notice did not isolate tool input")
						}
						if scenario == "attention_file" && !strings.Contains(body["prompt"].(string), "额外的文件或网络权限") {
							t.Error("wrong notice for native file request")
						}
					default:
						if !strings.Contains(body["prompt"].(string), "/synthetic/work") {
							t.Error("missing execution directory")
						}
					}
					if body["turn_id"] != "turn" || body["native_session"] != testID {
						t.Error("lost request origin")
					}
					state := "pending"
					if scenario == "attention_already_delivered" || scenario == "attention_retry" {
						state = "delivered"
					}
					jsonReply(w, 200, map[string]any{"id": registeredID, "state": state})
				case strings.HasSuffix(r.URL.Path, "/close"):
					if registeredID != "" && r.URL.Path != "/v1/session-relay/interactions/"+registeredID+"/close" {
						t.Error("closed wrong request")
					}
					closes++
					var body struct {
						Generation int64  `json:"generation"`
						State      string `json:"state"`
					}
					if json.NewDecoder(r.Body).Decode(&body) != nil || body.Generation != 1 || body.State != "cancelled" {
						t.Error("invalid closure scope")
					}
					jsonReply(w, 200, map[string]any{"id": "request", "state": "cancelled"})
				case r.Method == "GET" && r.URL.Path == "/v1/session-relay/interactions/"+registeredID:
					reads++
					state := "answered"
					answer := scenario
					if strings.HasPrefix(scenario, "attention") {
						state, answer = "delivered", ""
					}
					if scenario == "cancelled" || scenario == "attention_cancelled" {
						state = "cancelled"
						answer = ""
					}
					if scenario == "revoked" {
						answer = "allow"
						if reads > 1 {
							state = "cancelled"
						}
					}
					if scenario == "mixed" {
						answer = "allow"
					}
					if scenario == "question" {
						answer = "选择 B"
					}
					jsonReply(w, 200, map[string]any{"id": registeredID, "state": state, "answer": answer})
				default:
					t.Error("unexpected call", r.Method, r.URL.Path)
					w.WriteHeader(500)
				}
			})
			event, tool := "PermissionRequest", "Bash"
			if scenario == "question" || scenario == "secret" || scenario == "local_question" || scenario == "local_secret" {
				event = "PreToolUse"
				tool = "functions.request_user_input"
			}
			if strings.Contains(scenario, "attention") {
				event, tool = "PreToolUse", "mcp__cua_repl__js"
				if strings.HasSuffix(scenario, "_file") {
					tool = "request_permissions"
				}
			}
			input, _ := json.Marshal(map[string]any{"session_id": testID, "turn_id": "turn", "hook_event_name": event, "tool_name": tool, "cwd": "/synthetic/work", "tool_input": map[string]any{"description": "synthetic request", "questions": []any{map[string]any{"isSecret": scenario == "secret" || scenario == "local_secret", "title": "请选择", "options": []string{"A", "B"}}}}})
			var output bytes.Buffer
			if relayHook(dir, bytes.NewReader(input), &output) != 0 {
				t.Fatal("hook failed")
			}
			if scenario == "attention_retry" {
				if closes != 1 || !strings.Contains(output.String(), `"permissionDecision":"deny"`) {
					t.Fatalf("lost registration did not deny and close its attempt: %s, closes=%d", output.String(), closes)
				}
				output.Reset()
				if relayHook(dir, bytes.NewReader(input), &output) != 0 || strings.TrimSpace(output.String()) != "{}" || reads != 1 || closes != 1 {
					t.Fatalf("retry did not reuse and revalidate the delivered notice: %s, reads=%d, closes=%d", output.String(), reads, closes)
				}
				return
			}
			expectedCloses := 0
			if scenario == "mixed" || scenario == "revoked" || scenario == "cancelled" || scenario == "remote_failure" || scenario == "registration_lost" || scenario == "attention_cancelled" {
				expectedCloses = 1
			}
			if closes != expectedCloses {
				t.Fatalf("closed %d interactions, expected %d", closes, expectedCloses)
			}
			var response map[string]any
			if json.Unmarshal(output.Bytes(), &response) != nil {
				t.Fatal("not a single JSON hook response")
			}
			if strings.HasPrefix(scenario, "local_") || scenario == "origin_unavailable" || scenario == "unreadable" || scenario == "codex_unavailable" {
				if len(response) != 0 {
					t.Fatalf("local or unclassified turn interfered with: %s", output.String())
				}
				return
			}
			if scenario == "attention" || scenario == "attention_already_delivered" || scenario == "attention_file" {
				minimumReads := 2
				if scenario == "attention_already_delivered" {
					minimumReads = 1
				}
				if len(response) != 0 || reads != minimumReads {
					t.Fatalf("notice must be confirmed and revalidated: %s, reads=%d", output.String(), reads)
				}
				return
			}
			specific := response["hookSpecificOutput"].(map[string]any)
			if scenario == "attention_cancelled" {
				if specific["permissionDecision"] != "deny" {
					t.Fatal("native tool proceeded without notice")
				}
				return
			}
			if scenario == "secret" {
				if specific["permissionDecision"] != "deny" {
					t.Fatal("secret question not blocked")
				}
			} else if scenario == "question" {
				if specific["permissionDecision"] != "deny" || !strings.Contains(specific["permissionDecisionReason"].(string), "选择 B") {
					t.Fatal("question not intercepted with remote answer")
				}
			} else {
				decision := specific["decision"].(map[string]any)["behavior"]
				expected := "deny"
				if scenario == "allow" {
					expected = "allow"
				}
				if decision != expected {
					t.Fatalf("%s returned %v", scenario, decision)
				}
			}
		})
	}
}

func TestHookDiagnosticsDoNotExposeInput(t *testing.T) {
	var output, diagnostics bytes.Buffer
	relayHookDiagnosed(t.TempDir(), strings.NewReader(`{"turn_id":"turn","session_id":"session","hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"secret-marker"}}`), &output, &diagnostics)
	var event map[string]string
	if err := json.Unmarshal(diagnostics.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	if event["stage"] != "connection" || event["disposition"] != "native" {
		t.Fatalf("missing failure stage: %v", event)
	}
	if strings.Contains(diagnostics.String(), "secret-marker") || strings.Contains(diagnostics.String(), "session") || strings.Contains(diagnostics.String(), "turn") {
		t.Fatal("diagnostics exposed hook input")
	}
	if strings.TrimSpace(output.String()) != "{}" {
		t.Fatal("diagnostics changed native fallback")
	}
}

func TestPermissionPromptSeparatesCommandAndReason(t *testing.T) {
	input, _ := json.Marshal(map[string]any{"command": "printf '%s' \"test value\"", "description": "创建一个测试文件", "additionalPermissions": map[string]bool{"network": false}})
	prompt, err := interactionPrompt(relayHookInput{Event: "PermissionRequest", Tool: "Bash", Cwd: "/test/project", Input: input})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "```bash\nprintf '%s' \"test value\"\n```") || !strings.Contains(prompt, "创建一个测试文件") {
		t.Fatalf("command and reason are not readable: %s", prompt)
	}
	if strings.Contains(prompt, `"command":`) || !strings.Contains(prompt, "additionalPermissions") || !strings.Contains(prompt, "/test/project") {
		t.Fatal("request fields lost or raw command JSON shown")
	}
}

func TestAsyncQuestionHookReturnsBeforeUserAnswer(t *testing.T) {
	calls := 0
	dir, _ := testSetup(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		jsonReply(w, 500, map[string]any{})
	})
	input := `{"session_id":"` + testID + `","turn_id":"turn","hook_event_name":"PreToolUse","tool_name":"functions.request_user_input_async","tool_input":{"questions":[{"title":"颜色？"}]}}`
	var out bytes.Buffer
	if code := relayHook(dir, strings.NewReader(input), &out); code != 0 || strings.TrimSpace(out.String()) != "{}" || calls != 0 {
		t.Fatalf("async hook blocked native creation: calls=%d code=%d output=%s", calls, code, out.String())
	}
}
