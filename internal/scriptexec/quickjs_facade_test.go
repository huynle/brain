package scriptexec

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// Read the public implementation, not a second manually duplicated inventory.
// RequestOptions/transport/identity methods intentionally are not script methods.
func publicFacadeNames(t *testing.T) []string {
	t.Helper()
	source, err := os.ReadFile("../../sdk/typescript/src/index.ts")
	if err != nil {
		t.Fatal(err)
	}
	namespace, names := "", []string{"health", "inject", "search"}
	ns := regexp.MustCompile(`^  readonly (\w+) = Object.freeze\(\{`)
	method := regexp.MustCompile(`^    (\w+): `)
	for _, line := range strings.Split(string(source), "\n") {
		if m := ns.FindStringSubmatch(line); m != nil {
			namespace = m[1]
		}
		if line == "  });" {
			namespace = ""
		}
		if m := method.FindStringSubmatch(line); namespace != "" && m != nil {
			names = append(names, namespace+"."+m[1])
		}
	}
	sort.Strings(names)
	if len(names) != 105 {
		t.Fatalf("SDK inventory changed: %d methods", len(names))
	}
	return names
}

func TestQuickJSFacadeClosedInventory(t *testing.T) {
	names := publicFacadeNames(t)
	_, err := quickJSProgram(t, "", true, func(host, container string) ([]byte, error) {
		encoded, _ := json.Marshal(names)
		source := `const expected=` + string(encoded) + `;
const actual=[];
for(const ns of Object.keys(brain)) {
 if(typeof brain[ns]==="function") actual.push(ns);
 else for(const method of Object.keys(brain[ns])) actual.push(ns+"."+method);
}
if(JSON.stringify(actual.sort())!==JSON.stringify(expected)) throw "inventory";
if(!Object.isFrozen(brain)||Object.getPrototypeOf(brain)!==null) throw "root mutable";
for(const ns of Object.keys(brain)) if(typeof brain[ns]!=="function" &&
 (!Object.isFrozen(brain[ns])||Object.getPrototypeOf(brain[ns])!==null)) throw "namespace mutable";
let denied=0;
for(const path of expected) {
 if(path==="entries.get") continue;
 const parts=path.split(".");const fn=parts.length===1?brain[parts[0]]:brain[parts[0]][parts[1]];
 try { await fn(); throw "unexpected permission"; }
 catch(e) { if(e.code!=="unsupported_operation"||e.message!=="unsupported_operation") throw "unsafe error"; denied++; }
}
if(brain.request!==undefined||brain.fetch!==undefined||brain.entries.constructor!==undefined) throw "escape hatch";
({count:expected.length,denied});`
		out := runFacadeFixture(t, host, container, source)
		parent, err := NewProtocolSession(ProtocolLimits{100, 65536, 1 << 20})
		if err != nil {
			t.Fatal(err)
		}
		r := bytes.NewReader(out)
		frame, err := ReadFrame(r)
		if err != nil {
			t.Fatalf("facade inventory produced no terminal result: %v", err)
		}
		message, err := parent.Accept(frame)
		if err != nil || !message.Done || string(message.Result) != `{"count":105,"denied":104}` || r.Len() != 0 {
			t.Fatalf("facade must have exact SDK inventory, closed namespaces, explicit denials and zero IPC calls: %s err=%v trailing=%d", message.Result, err, r.Len())
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func runFacadeFixture(t *testing.T, host, container, source string, replies ...json.RawMessage) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "--host", host, "exec", "-i", "--user=65534:65534", container, "/usr/bin/env", "-i", "/tmp/probe")
	cmd.WaitDelay = time.Second
	var in, diagnostics bytes.Buffer
	text, _ := json.Marshal(source)
	if err := WriteFrame(&in, Frame{1, "call", 1, text}); err != nil {
		t.Fatal(err)
	}
	for i, reply := range replies {
		if err := WriteFrame(&in, Frame{1, "result", uint64(i + 1), reply}); err != nil {
			t.Fatal(err)
		}
	}
	cmd.Stdin, cmd.Stderr = &in, &diagnostics
	out, err := cmd.Output()
	if ctx.Err() != nil || diagnostics.Len() != 0 {
		t.Fatal("deadline or protected diagnostic leak")
	}
	if err != nil {
		t.Fatalf("facade program refused: %v (stdout bytes=%d)", err, len(out))
	}
	return out
}

func TestQuickJSFacadeArgumentBoundary(t *testing.T) {
	_, err := quickJSProgram(t, "", true, func(host, container string) ([]byte, error) {
		for _, tc := range []struct {
			name, source, want string
			call               bool
		}{
			{"optional undefined", `brain.entries.get("one",undefined).value;`, `42`, true},
			{"empty identifier", `try{brain.entries.get("");}catch(e){e.code;}`, `"invalid_arguments"`, false},
			{"missing identifier", `try{brain.entries.get();}catch(e){e.code;}`, `"invalid_arguments"`, false},
			{"no coercion", `let n=0;try{brain.entries.get({toString(){n++;return "one"}});}catch(e){({code:e.code,n});}`, `{"code":"invalid_arguments","n":0}`, false},
			{"no transport options", `try{brain.entries.get("one",{token:"secret"});}catch(e){e.code;}`, `"invalid_arguments"`, false},
			{"too many arguments", `try{brain.entries.get("one",undefined,undefined);}catch(e){e.code;}`, `"invalid_arguments"`, false},
			{"unsupported never inspects arguments", `let n=0;try{brain.entries.create({get content(){n++;throw "secret"},toJSON(){n++;throw "secret"}});}catch(e){({code:e.code,n});}`, `{"code":"unsupported_operation","n":0}`, false},
			{"unsupported ignores prototype", `Object.prototype.code="forged";Object.prototype.toJSON=()=>"secret";try{brain.search({});}catch(e){({toJSON(){return e.code}});}`, `"unsupported_operation"`, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				replies := []json.RawMessage{}
				if tc.call {
					replies = append(replies, json.RawMessage(`{"value":42}`))
				}
				r := bytes.NewReader(runFacadeFixture(t, host, container, tc.source, replies...))
				parent, err := NewProtocolSession(ProtocolLimits{100, 65536, 1 << 20})
				if err != nil {
					t.Fatal(err)
				}
				if tc.call {
					frame, err := ReadFrame(r)
					if err != nil {
						t.Fatal(err)
					}
					message, err := parent.Accept(frame)
					if err != nil || message.Call == nil || message.Call.Operation != "entries.get" || string(message.Call.Arguments) != `{"id":"one"}` {
						t.Fatalf("unexpected call %+v err=%v", message, err)
					}
					if _, err = parent.Reply(replies[0]); err != nil {
						t.Fatal(err)
					}
				}
				frame, err := ReadFrame(r)
				if err != nil {
					t.Fatal(err)
				}
				message, err := parent.Accept(frame)
				if err != nil || !message.Done || string(message.Result) != tc.want || r.Len() != 0 {
					t.Fatalf("result %s want %s err=%v trailing=%d", message.Result, tc.want, err, r.Len())
				}
			})
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
