package scriptexec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"unicode/utf8"
)

var errProtectedOutput = errors.New("protected output unavailable")

// Inactive holding area, NOT a release fence or durable audit. There is no release
// API: S09/S17 must supply that protocol. Only the trusted parent records sources;
// worker-supplied source assertions are never accepted. The complete source union
// applies to every log/result, including sources read after an earlier log.
// Do not copy after first use. Formatting deliberately never inspects its fields.
type outputQuarantine struct {
	*quarantineState
}

type quarantineState struct {
	mu                    sync.Mutex
	logs                  [][]byte
	result                []byte
	sources               map[string]bool
	logBytes, sourceBytes int
	closed, complete      bool
}

func newOutputQuarantine() *outputQuarantine {
	return &outputQuarantine{quarantineState: &quarantineState{}}
}

func (outputQuarantine) Format(s fmt.State, _ rune)   { _, _ = io.WriteString(s, "[protected output]") }
func (outputQuarantine) MarshalJSON() ([]byte, error) { return []byte(`{"protected":true}`), nil }

func (q *outputQuarantine) addLog(data []byte) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	bad := func() error { q.clearLocked(); return errProtectedOutput }
	if q.closed || q.complete || len(q.logs) >= 32 || len(data) > 8192 || len(data) > 16384-q.logBytes || !quarantineJSON(data) {
		return bad()
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || len(fields) != 2 {
		return bad()
	}
	var level string
	if json.Unmarshal(fields["level"], &level) != nil {
		return bad()
	}
	switch level {
	case "debug", "info", "warn", "error", "log":
	default:
		return bad()
	}
	values := bytes.TrimSpace(fields["values"])
	if len(values) == 0 || values[0] != '[' {
		return bad()
	}
	q.logs = append(q.logs, bytes.Clone(data))
	q.logBytes += len(data)
	return nil
}

func (q *outputQuarantine) addSource(source string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed || q.complete || source == "" || len(source) > 4096 || !utf8.ValidString(source) {
		q.clearLocked()
		return errProtectedOutput
	}
	if q.sources[source] {
		return nil
	}
	if len(q.sources) >= 1000 || len(source) > 65536-q.sourceBytes {
		q.clearLocked()
		return errProtectedOutput
	}
	if q.sources == nil {
		q.sources = make(map[string]bool)
	}
	q.sources[source] = true
	q.sourceBytes += len(source)
	return nil
}

func (q *outputQuarantine) setResult(data []byte) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed || q.complete || len(data) > 65536 || !quarantineJSON(data) {
		q.clearLocked()
		return errProtectedOutput
	}
	q.result = bytes.Clone(data)
	q.complete = true
	return nil
}

func (q *outputQuarantine) retire() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.clearLocked()
}
func (q *outputQuarantine) clearLocked() {
	for _, log := range q.logs {
		clear(log)
	}
	clear(q.result)
	clear(q.sources)
	q.logs = nil
	q.result = nil
	q.sources = nil
	q.logBytes = 0
	q.sourceBytes = 0
	q.closed = true
}

func quarantineJSON(data []byte) bool {
	if len(data) == 0 || !utf8.Valid(data) {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if !uniqueJSON(d, 0) {
		return false
	}
	_, err := d.Token()
	return err == io.EOF
}
