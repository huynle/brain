package runner

import (
	"bufio"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestReadLineBounded_ShortLinesAndCRLF(t *testing.T) {
	r := bufio.NewReaderSize(strings.NewReader("data: {\"a\":1}\r\n\nevent: x\ndata: {\"b\":2}"), 16)
	var got []string
	for {
		line, tooLong, err := readLineBounded(r, 1024)
		if tooLong {
			t.Fatal("no line here is oversized")
		}
		if len(line) > 0 {
			got = append(got, string(line))
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				t.Fatalf("unexpected error: %v", err)
			}
			break
		}
	}
	want := []string{`data: {"a":1}`, `event: x`, `data: {"b":2}`}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("lines = %q, want %q", got, want)
	}
}

func TestReadLineBounded_OversizedLineIsDroppedAndStreamContinues(t *testing.T) {
	// A 3 MB event followed by a small control event: the big one must be
	// drained and reported, and the small one must still arrive intact. This
	// is the exact failure the 1 MB bufio.Scanner had — it returned ErrTooLong
	// and the caller tore the stream down.
	big := "data: " + strings.Repeat("x", 3*1024*1024) + "\n"
	small := "data: {\"type\":\"permission.asked\"}\n"
	r := bufio.NewReaderSize(strings.NewReader(big+small), 64*1024)

	line, tooLong, err := readLineBounded(r, 1024*1024)
	if err != nil || !tooLong || line != nil {
		t.Fatalf("first read: line=%d bytes tooLong=%v err=%v; want dropped", len(line), tooLong, err)
	}
	line, tooLong, err = readLineBounded(r, 1024*1024)
	if err != nil || tooLong {
		t.Fatalf("second read: tooLong=%v err=%v", tooLong, err)
	}
	if string(line) != strings.TrimSuffix(small, "\n") {
		t.Errorf("second line = %q", line)
	}
	if _, _, err := readLineBounded(r, 1024*1024); !errors.Is(err, io.EOF) {
		t.Errorf("expected EOF after the two lines, got %v", err)
	}
}

func TestReadLineBounded_ExactLimitIsKept(t *testing.T) {
	payload := strings.Repeat("y", 100)
	r := bufio.NewReaderSize(strings.NewReader(payload+"\n"), 16)
	line, tooLong, err := readLineBounded(r, 100)
	if err != nil || tooLong || string(line) != payload {
		t.Fatalf("a line exactly at the limit must be kept: tooLong=%v err=%v len=%d", tooLong, err, len(line))
	}
}

func TestReadLineBounded_OversizedUnterminatedAtEOF(t *testing.T) {
	r := bufio.NewReaderSize(strings.NewReader(strings.Repeat("z", 5000)), 16)
	line, tooLong, err := readLineBounded(r, 1000)
	if !tooLong || line != nil || !errors.Is(err, io.EOF) {
		t.Fatalf("got line=%d tooLong=%v err=%v", len(line), tooLong, err)
	}
}
