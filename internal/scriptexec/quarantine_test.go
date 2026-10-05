package scriptexec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestOutputQuarantineBoundsAndErasure(t *testing.T) {
	for _, tc := range []struct {
		name    string
		input   []byte
		repeats int
	}{
		{"count", []byte(`{"level":"log","values":["secret"]}`), 33},
		{"entry bytes", []byte(`{"level":"log","values":["` + strings.Repeat("x", 8192) + `"]}`), 1},
		{"total bytes", []byte(`{"level":"log","values":["` + strings.Repeat("x", 6000) + `"]}`), 3},
		{"unknown field", []byte(`{"level":"log","values":[],"sources":["forged"]}`), 1},
		{"duplicate", []byte(`{"level":"log","level":"warn","values":[]}`), 1},
		{"level", []byte(`{"level":"trace","values":[]}`), 1},
		{"values", []byte(`{"level":"log","values":null}`), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := newOutputQuarantine()
			var err error
			for i := 0; i < tc.repeats; i++ {
				err = q.addLog(tc.input)
			}
			if err == nil {
				t.Fatal("untrusted console was not bounded/refused")
			}
			if len(q.logs) != 0 || len(q.result) != 0 {
				t.Fatal("retired quarantine kept protected bytes")
			}
			if q.addLog([]byte(`{"level":"log","values":[]}`)) == nil {
				t.Fatal("retired quarantine resurrected")
			}
		})
	}
}

func TestOutputQuarantineSourcesCopiesAndRedaction(t *testing.T) {
	q := newOutputQuarantine()
	if err := q.addSource("entry:secret-resource"); err != nil {
		t.Fatal(err)
	}
	input := []byte(`{"level":"warn","values":["secret-content"]}`)
	if err := q.addLog(input); err != nil {
		t.Fatal(err)
	}
	if len(q.logs) != 1 || len(q.sources) != 1 {
		t.Fatal("quarantine lost log or trusted source")
	}
	input[0] = 'x'
	if !json.Valid(q.logs[0]) {
		t.Fatal("input aliases protected buffer")
	}
	if err := q.addSource("entry:later-source"); err != nil {
		t.Fatal(err)
	}
	result := []byte(`{"secret":42}`)
	if err := q.setResult(result); err != nil {
		t.Fatal(err)
	}
	result[0] = 'x'
	if !json.Valid(q.result) || len(q.sources) != 2 {
		t.Fatal("result/source union lost")
	}
	for _, v := range []any{q, *q} {
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			if strings.Contains(fmt.Sprintf(format, v), "secret") {
				t.Fatal("format disclosed protected data")
			}
		}
		b, err := json.Marshal(v)
		if err != nil || bytes.Contains(b, []byte("secret")) {
			t.Fatal("JSON disclosed protected data")
		}
	}
	heldLog, heldResult := q.logs[0], q.result
	q.retire()
	if len(q.sources) != 0 || len(q.logs) != 0 || len(q.result) != 0 {
		t.Fatal("retirement retained state")
	}
	if len(bytes.Trim(heldLog, "\x00")) != 0 {
		t.Fatal("log bytes not overwritten")
	}
	if len(bytes.Trim(heldResult, "\x00")) != 0 {
		t.Fatal("result bytes not overwritten")
	}
}

func TestOutputQuarantineConcurrentRetirement(t *testing.T) {
	q := newOutputQuarantine()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = q.addLog([]byte(`{"level":"info","values":[]}`))
			_ = q.addSource("entry:one")
			q.retire()
		}()
	}
	wg.Wait()
	if q.setResult([]byte(`42`)) == nil {
		t.Fatal("retired result accepted")
	}
}
