package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

func readerOrigin(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("base_url must be an http(s) origin without credentials, path, query, or fragment")
	}
	return u, nil
}

func registerReaderURL(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "reader_url",
		Description: "Resolve an entry by path or short ID and return its standalone Markdown reader URL, title, and canonical path. The entry is verified through the connected Brain API with your credentials. The link grants no access and requires a deployment with reader support. Defaults to the MCP request origin for HTTP, or configured API origin for stdio. Optional base_url selects a hosted/local origin; no request is made to the override and its entry availability is not verified. Localhost links work only on the device running that instance.",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"path":     {Type: "string", Description: "Entry path or 8-character entry ID"},
			"base_url": {Type: "string", Description: "Optional origin, e.g. https://brain.huynle.com or http://localhost:3333"},
		}, Required: []string{"path"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		path := strings.TrimSpace(StringArg(args, "path", ""))
		if path == "" {
			return "", fmt.Errorf("path is required (entry path or short ID)")
		}
		base := client.readerBaseURL
		if base == "" {
			base = client.baseURL
		}
		base = StringArg(args, "base_url", base)
		u, err := readerOrigin(base)
		if err != nil {
			return "", err
		}
		var entry struct {
			ID    string `json:"id"`
			Path  string `json:"path"`
			Title string `json:"title"`
		}
		parts := strings.Split(path, "/")
		for i := range parts {
			parts[i] = url.PathEscape(parts[i])
		}
		if err = client.Request(ctx, "GET", "/entries/"+strings.Join(parts, "/"), nil, nil, &entry); err != nil {
			return "", err
		}
		if entry.Path == "" {
			return "", fmt.Errorf("entry response did not contain a canonical path")
		}
		u.Path = "/read.html"
		u.RawPath = ""
		u.RawQuery = url.Values{"entry": {entry.Path}}.Encode()
		out, err := json.Marshal(map[string]string{"id": entry.ID, "path": entry.Path, "title": entry.Title, "reader_url": u.String()})
		return string(out), err
	})
}
