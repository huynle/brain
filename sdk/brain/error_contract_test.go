package brain_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/huynle/brain-api/sdk/brain"
	"net/http"
	"strings"
	"testing"
)

func TestLegacyErrorOnlyConflictMessage(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`{"success":false,"error":"private claim holder"}`, "private claim holder"},
		{`{"error":"private fallback","message":"private primary"}`, "private primary"},
		{`{"error":"private fallback","message":""}`, "private fallback"},
		{`{"error":{"secret":"private"}}`, ""},
	} {
		t.Run(tc.body, func(t *testing.T) {
			c := client(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Request-ID", "conflict-request")
				w.WriteHeader(409)
				fmt.Fprint(w, tc.body)
			}, brain.Config{})
			_, err := c.Tasks().Dispatch(context.Background(), "p", "t", brain.DispatchRequest{TargetRunnerId: "r"}, brain.RequestOptions{})
			var got *brain.Error
			if !errors.As(err, &got) || got.Code != "conflict" || got.Message != tc.want || got.RequestID != "conflict-request" {
				t.Fatalf("got message=%q want=%q error=%v", got.Message, tc.want, err)
			}
			if strings.Contains(fmt.Sprint(err), "private") || strings.Contains(fmt.Sprintf("%+v", err), "private") {
				t.Fatal("default error leaked content")
			}
		})
	}
}
