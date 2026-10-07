package brain_test

import (
	"bytes"
	"context"
	"errors"
	"github.com/huynle/brain-api/sdk/brain"
	"io"
	"net/http"
	"testing"
)

func TestAttachmentBytesRoundTrip(t *testing.T) {
	content := []byte{0, 255, 1, 2}
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			if err := r.ParseMultipartForm(1024); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			defer r.MultipartForm.RemoveAll()
			if r.FormValue("project_id") != "p" || r.FormValue("metadata") != `{"source":"sdk"}` {
				t.Error("multipart fields lost")
			}
			f, h, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				return
			}
			defer f.Close()
			got, _ := io.ReadAll(f)
			if h.Filename != "bytes.bin" || h.Header.Get("Content-Type") != "application/octet-stream" || !bytes.Equal(got, content) {
				t.Error("file corrupted")
			}
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{"attachment":{"id":"a"}}`))
			return
		}
		if r.URL.RequestURI() != "/api/v1/attachments/a/content?project_id=p" {
			t.Error(r.URL.RequestURI())
		}
		_, _ = w.Write(content)
	}, brain.Config{})
	uploaded, err := c.Attachments().Upload(context.Background(), "p", brain.UploadRequest{Filename: "bytes.bin", ContentType: "application/octet-stream", Content: content, Metadata: map[string]string{"source": "sdk"}}, brain.RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if uploaded.Attachment.Id != "a" {
		t.Fatalf("%+v", uploaded)
	}
	got, err := c.Attachments().Download(context.Background(), "p", "a")
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("%v %v", got, err)
	}
}

func TestAttachmentBoundsAndInvalidFilename(t *testing.T) {
	calls := 0
	c := client(t, func(w http.ResponseWriter, _ *http.Request) { calls++; _, _ = w.Write(bytes.Repeat([]byte{1}, 33)) }, brain.Config{MaxResponseBytes: 32})
	for _, r := range []brain.UploadRequest{{Filename: "../secret", Content: []byte("x")}, {Filename: "x\r\nY: z"}, {Filename: "x", Content: make([]byte, 33)}} {
		_, err := c.Attachments().Upload(context.Background(), "p", r, brain.RequestOptions{})
		var e *brain.Error
		if !errors.As(err, &e) || (e.Code != "invalid_request" && e.Code != "request_too_large") {
			t.Fatalf("%v", err)
		}
	}
	if calls != 0 {
		t.Fatal("invalid upload reached network")
	}
	_, err := c.Attachments().Download(context.Background(), "p", "a")
	var e *brain.Error
	if !errors.As(err, &e) || e.Code != "response_too_large" {
		t.Fatalf("%v", err)
	}
}

func TestAttachmentDerivedTextRoutes(t *testing.T) {
	var got []string
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.RequestURI())
		if r.Method == "GET" {
			_, _ = w.Write([]byte("derived text"))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}, brain.Config{})
	if _, err := c.Attachments().Extract(context.Background(), "p", "a", brain.AttachmentExtractionRequest{}, brain.RequestOptions{}); err != nil {
		t.Fatal(err)
	}
	text, err := c.Attachments().Text(context.Background(), "p", "a")
	if err != nil || text != "derived text" {
		t.Fatalf("%q %v", text, err)
	}
	if len(got) != 2 || got[0] != "POST /api/v1/attachments/a/extract?project_id=p" || got[1] != "GET /api/v1/attachments/a/text?project_id=p" {
		t.Fatalf("%v", got)
	}
}
