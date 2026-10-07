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

func TestOutputQuarantineExactSourceBudgets(t *testing.T) {
	for _, tc := range []struct {
		name         string
		count, width int
	}{
		{"source count", 1000, 8}, {"source bytes", 16, 4096},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := newOutputQuarantine()
			for i := 0; i < tc.count; i++ {
				source := fmt.Sprintf("%04d", i) + strings.Repeat("x", tc.width-4)
				if err := q.addSource(source); err != nil {
					t.Fatalf("exact boundary rejected at%d: %v", i, err)
				}
				if err := q.addSource(source); err != nil {
					t.Fatal("duplicate source double-counted")
				}
			}
			if len(q.sources) != tc.count || q.sourceBytes != tc.count*tc.width {
				t.Fatal("source accounting differs")
			}
			if err := q.addLog([]byte(`{"level":"log","values":["protected"]}`)); err != nil {
				t.Fatal(err)
			}
			held := q.logs[0]
			if q.addSource("overflow") == nil {
				t.Fatal("source overflow allowed")
			}
			if len(q.sources) != 0 || q.sourceBytes != 0 || len(bytes.Trim(held, "\x00")) != 0 {
				t.Fatal("source failure retained protected state")
			}
			if q.setResult([]byte(`42`)) == nil {
				t.Fatal("source-overflow quarantine resurrected")
			}
		})
	}
	for _, source := range []string{"", strings.Repeat("x", 4097), "\xff"} {
		q := newOutputQuarantine()
		if q.addSource(source) == nil || !q.closed {
			t.Fatal("invalid source accepted")
		}
	}
}

func TestOutputQuarantineExactResultAndJSONBounds(t *testing.T) {
	for _, size := range []int{65536, 65537} {
		q := newOutputQuarantine()
		result := []byte(`"` + strings.Repeat("x", size-2) + `"`)
		err := q.setResult(result)
		if (err == nil) != (size == 65536) {
			t.Fatalf("result boundary%d: %v", size, err)
		}
		if size == 65536 {
			held := q.result
			if q.addSource("late-source") == nil {
				t.Fatal("late source accepted after finalization")
			}
			if len(bytes.Trim(held, "\x00")) != 0 {
				t.Fatal("late source did not invalidate result")
			}
		}
	}
	for _, depth := range []int{64, 65} {
		q := newOutputQuarantine()
		err := q.setResult([]byte(strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth)))
		if (err == nil) != (depth == 64) {
			t.Fatalf("depth%d boundary: %v", depth, err)
		}
	}
	for _, data := range [][]byte{[]byte(`{"x":1,"\u0078":2}`), []byte("\"\xff\""), []byte(`42 true`), []byte(``)} {
		q := newOutputQuarantine()
		if q.setResult(data) == nil || !q.closed {
			t.Fatal("invalid result accepted")
		}
	}
}

func TestOutputQuarantineFormattingDuringRetirement(t *testing.T) {
	q := newOutputQuarantine()
	if err := q.addSource("secret-source"); err != nil {
		t.Fatal(err)
	}
	if err := q.setResult([]byte(`"secret-result"`)); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 100; j++ {
				if fmt.Sprintf("%#v", q) != "[protected output]" {
					t.Error("format disclosure")
				}
				encoded, err := json.Marshal(q)
				if err != nil || string(encoded) != `{"protected":true}` {
					t.Error("JSON disclosure")
				}
			}
		}()
	}
	close(start)
	q.retire()
	wg.Wait()
}
