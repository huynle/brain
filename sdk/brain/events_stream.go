package brain

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"net/url"
	"strings"
)

// Stream consumes Event data until cancellation, EOF or callback error.
func (s EventsService) Stream(ctx context.Context, query url.Values, lastEventID string, onEvent func(Event) error, options RequestOptions) error {
	if onEvent == nil || strings.ContainsAny(lastEventID, "\r\n\x00") {
		return &Error{Code: "invalid_request"}
	}
	return s.c.request(ctx, "GET", "/events/stream", nil, query, options, &eventStream{lastEventID: lastEventID, onEvent: onEvent})
}

type eventStream struct {
	lastEventID string
	onEvent     func(Event) error
}

func (s *eventStream) read(ctx, lifetime context.Context, resp *http.Response, limit int64) error {
	// AfterFunc still interrupts blocked network reads, but its asynchronous
	// propagation cannot fence already-buffered output from a retired binding.
	canceled := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return lifetime.Err()
	}
	if err := canceled(); err != nil {
		return err
	}
	failure := func(code string) error {
		return &Error{Code: code, Status: resp.StatusCode, RequestID: resp.Header.Get("X-Request-ID")}
	}
	media, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || media != "text/event-stream" {
		return failure("invalid_response")
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, min(4096, int(limit)+1)), int(limit)+1)
	var data strings.Builder
	size := 0
	for {
		if err := canceled(); err != nil {
			return err
		}
		if !scanner.Scan() {
			break
		}
		if err := canceled(); err != nil {
			return err
		}
		line := scanner.Text()
		size += len(line) + 1
		if int64(size) > limit {
			return failure("response_too_large")
		}
		if line == "" {
			if data.Len() > 0 {
				var event Event
				if json.Unmarshal([]byte(data.String()), &event) != nil || event.Id == "" || event.Type == "" || event.Source == "" || event.Timestamp.IsZero() {
					return failure("invalid_response")
				}
				if err := canceled(); err != nil {
					return err
				}
				callbackErr := s.onEvent(event)
				if err := canceled(); err != nil {
					return err
				}
				if callbackErr != nil {
					return callbackErr
				}
			}
			data.Reset()
			size = 0
		} else if value, ok := strings.CutPrefix(line, "data:"); ok {
			value = strings.TrimPrefix(value, " ")
			data.WriteString(value)
			data.WriteByte('\n')
		}
	}
	if err := canceled(); err != nil {
		return err
	}
	if errors.Is(scanner.Err(), bufio.ErrTooLong) {
		return failure("response_too_large")
	}
	if scanner.Err() != nil {
		return failure("response_read_failed")
	}
	// An incomplete frame at EOF is not dispatched (SSE semantics).
	return nil
}
