package scriptexec

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

// These run in the real native sealed child, not Node or a facade mock. The
// parent supplies only canned replies; this is not service integration evidence.
func TestQuickJSScriptExamples(t *testing.T) {
	_, err := quickJSProgram(t, "", true, func(host, container string) ([]byte, error) {
		for _, tc := range []struct {
			file    string
			ids     []string
			replies []json.RawMessage
			want    string
		}{
			{"read-pair.js", []string{"first", "second"}, []json.RawMessage{json.RawMessage(`{"value":20}`), json.RawMessage(`{"value":22}`)}, `{"sum":42}`},
			{"unsupported-write.js", nil, nil, `{"code":"unsupported_operation"}`},
			{"unsupported-iterator.js", nil, nil, `{"code":"unsupported_operation"}`},
		} {
			t.Run(tc.file, func(t *testing.T) {
				source, err := os.ReadFile("testdata/examples/" + tc.file)
				if err != nil {
					t.Fatal(err)
				}
				r := bytes.NewReader(runFacadeFixture(t, host, container, string(source), tc.replies...))
				s, _ := NewProtocolSession(ProtocolLimits{100, 65536, 1 << 20})
				for i, id := range tc.ids {
					f, err := ReadFrame(r)
					if err != nil {
						t.Fatal(err)
					}
					m, err := s.Accept(f)
					want, _ := json.Marshal(map[string]string{"id": id})
					if err != nil || m.Call == nil || m.Call.Operation != "entries.get" || !bytes.Equal(m.Call.Arguments, want) {
						t.Fatalf("unexpected call %+v err=%v", m, err)
					}
					if _, err = s.Reply(tc.replies[i]); err != nil {
						t.Fatal(err)
					}
				}
				f, err := ReadFrame(r)
				if err != nil {
					t.Fatal(err)
				}
				m, err := s.Accept(f)
				if err != nil || !m.Done || string(m.Result) != tc.want || r.Len() != 0 {
					t.Fatalf("unexpected example result %+v err=%v trailing=%d", m, err, r.Len())
				}
			})
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
