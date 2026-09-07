package mcpserver

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/tenant"
)

func TestLocalTenantContext(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx, err := localTenantContext(parent)
	if err != nil {
		t.Fatal(err)
	}
	if id, ok := tenant.From(ctx); !ok || id != tenant.Local {
		t.Errorf("stdio tenant = %v, %v", id, ok)
	}
	cancel()
	if ctx.Err() != context.Canceled {
		t.Error("lost cancellation")
	}
}

func TestStdioRejectsForeignInheritedScope(t *testing.T) {
	ctx := tenant.Into(context.Background(), tenant.MustParse("foreign"))
	err := RunMCPServer(ctx, MCPOptions{APIURL: "https://remote.invalid"}, strings.NewReader(""), io.Discard)
	if err == nil {
		t.Fatal("stdio accepted foreign inherited tenant")
	}
}
