package apiserver

import (
	"context"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/tenant"
)

// One operation is one real graph acquisition. Fixture provisioning (100/300/500 mapped
// FTS tables in ONE SQLite file) is excluded. No embeddings/extraction/providers.
// Use a fixed iteration count for comparisons; this is not HTTP/search load acceptance.
func BenchmarkTenantGraphLoad(b *testing.B) {
	for _, tenants := range []int{100, 300, 500} {
		b.Run(fmt.Sprintf("tenants%d", tenants), func(b *testing.B) {
			benchmarkTenantGraphLoad(b, tenants)
		})
	}
}

func benchmarkTenantGraphLoad(b *testing.B, tenants int) {
	f := newGraphFixture(b, tenants)
	for _, capacity := range []int{16, 32} {
		for _, pattern := range []string{"cold", "uniform", "hot-set"} {
			b.Run(fmt.Sprintf("cap%d/%s", capacity, pattern), func(b *testing.B) {
				var builds atomic.Int64
				var constructNS atomic.Int64
				factory := tenantHTTPFactory(f.owner.ForTenant, f.roots, config.Config{})
				m, err := newTenantGraphManager(capacity, activeGraphAuthority, func(ctx context.Context, id tenant.ID) (graphResource, error) {
					start := time.Now()
					g, e := factory(ctx, id)
					constructNS.Add(time.Since(start).Nanoseconds())
					builds.Add(1)
					return g, e
				})
				if err != nil {
					b.Fatal(err)
				}
				b.Cleanup(func() {
					if err := m.Shutdown(context.Background()); err != nil {
						b.Error(err)
					}
				})
				// Warm one graph to remove first-use package initialization. Root
				// resolution still reads ALL persisted mappings on every miss.
				for _, id := range f.ids[:1] {
					l, e := m.Acquire(context.Background(), id)
					if e != nil {
						b.Fatal(e)
					}
					l.Release()
				}
				for _, id := range f.ids {
					m.Invalidate(id)
				}
				// Recreate an empty manager after joined shutdown for cold-start counts.
				if err := m.Shutdown(context.Background()); err != nil {
					b.Fatal(err)
				}
				m, err = newTenantGraphManager(capacity, activeGraphAuthority, func(ctx context.Context, id tenant.ID) (graphResource, error) {
					start := time.Now()
					g, e := factory(ctx, id)
					constructNS.Add(time.Since(start).Nanoseconds())
					builds.Add(1)
					return g, e
				})
				if err != nil {
					b.Fatal(err)
				}
				builds.Store(0)
				constructNS.Store(0)
				runtime.GC()
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				maxLive := 0
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					index := i % tenants
					if pattern == "uniform" {
						index = (i * 137) % tenants
					}
					if pattern == "hot-set" {
						index = i % 20
						if i%10 == 0 {
							index = 20 + (i/10)%(tenants-20)
						}
					}
					id := f.ids[index]
					if pattern == "cold" {
						m.Invalidate(id)
					}
					l, err := m.Acquire(context.Background(), id)
					if err != nil {
						b.Fatal(err)
					}
					m.mu.Lock()
					live := len(m.entries)
					m.mu.Unlock()
					if live > maxLive {
						maxLive = live
					}
					if live > capacity {
						b.Fatal("unbounded live graphs")
					}
					l.Release()
				}
				b.StopTimer()
				runtime.GC()
				runtime.ReadMemStats(&after)
				m.mu.Lock()
				resident := len(m.entries)
				m.mu.Unlock()
				retained := int64(after.HeapAlloc) - int64(before.HeapAlloc)
				b.ReportMetric(float64(retained)/float64(resident), "retained-B/graph")
				b.ReportMetric(float64(maxLive), "max-live")
				b.ReportMetric(100*(1-float64(builds.Load())/float64(b.N)), "hit-%")
				b.ReportMetric(float64(constructNS.Load())/float64(builds.Load()), "construct-ns/miss")
				b.ReportMetric(float64(builds.Load()), "builds")
				runtime.KeepAlive(m)
			})
		}
	}
}
