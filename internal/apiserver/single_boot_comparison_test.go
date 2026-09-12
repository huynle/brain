package apiserver

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/tenant"
)

// BenchmarkSingleBootComparison is deliberately compatible with ce103b16.
// Run each case with -benchtime=1x -count=1 in a fresh process: the old
// composition does not join all workers on cleanup. Process exit prevents those
// workers from contaminating subsequent samples. See scripts/compare-single-boot.py.
func BenchmarkSingleBootComparison(b *testing.B) {
	if b.N != 1 {
		b.Fatal("requires -benchtime=1x and an independent process per sample")
	}
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(logger)
	for _, fixture := range []string{"fresh-empty", "existing-empty"} {
		b.Run(fixture, func(b *testing.B) {
			b.StopTimer()
			if b.N != 1 {
				b.Fatal("requires -benchtime=1x")
			}
			root := b.TempDir()
			if fixture == "existing-empty" {
				if err := os.MkdirAll(filepath.Join(root, ".brain-data"), 0o755); err != nil {
					b.Fatal(err)
				}
				// Seed only storage and roots, not a previous running handler.
				// Thus both binaries start with v28 and identical logical contents,
				// without prior startup workers leaking into the measured operation.
				views, err := openSingleModeStorage(context.Background(), tenant.ModeSingle,
					filepath.Join(root, ".brain-data", "brain.db"), root, false)
				if err != nil {
					b.Fatal(err)
				}
				_, err = views.roots.ProvisionLocal(context.Background(), root, filepath.Join(root, "attachments"))
				views.close()
				if err != nil {
					b.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			opts := ServerOptions{Host: "127.0.0.1", BrainDir: root,
				Tenancy: config.TenancyConfig{Mode: tenant.ModeSingle}}
			b.ReportAllocs()
			b.ResetTimer()
			b.StartTimer()
			handler, dbPath, cleanup, err := buildHTTPHandler(ctx, opts)
			b.StopTimer()
			if err != nil {
				b.Fatal(err)
			}
			defer cleanup()
			if dbPath != filepath.Join(root, ".brain-data", "brain.db") {
				b.Fatalf("unexpected DB path: %s", dbPath)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
			if response.Code != http.StatusOK {
				b.Fatalf("health: %d %s", response.Code, response.Body.String())
			}
			cancel()
		})
	}
}
