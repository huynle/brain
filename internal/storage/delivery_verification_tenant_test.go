package storage

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/types"
)

func TestTenantDeliveryVerificationCAS(t *testing.T) {
	db := successorPrivateFixture(t)
	for _, id := range []string{"a", "b"} {
		_, err := db.Exec("INSERT INTO notes(tenant_id,path,short_id,metadata) VALUES(?,'delivery','delivery','{\"unrelated\":true}')", id)
		collisionMust(t, err)
		s := &TenantStore{db: db, tenantID: tenant.MustParse(id)}
		ctx := tenant.Into(context.Background(), s.TenantID())
		_, err = s.ReserveSyncOperation(ctx, "delivery", "hash")
		collisionMust(t, err)
		collisionMust(t, s.CompleteSyncOperation(ctx, "delivery", 201, `{"preserve":true}`))
		collisionMust(t, s.SaveSyncDevice(ctx, nil, types.SyncDevice{ID: "delivery", Owner: id, Pending: []types.SyncPending{{ID: "draft", Raw: id}}}))
	}
	// Includes B's notes, all successor ledgers/four sync tables, FTS mapping,
	// and all five FTS shadow tables, not just the colliding note's metadata.
	foreign := syncSnapshot(t, db, "b")
	a := &TenantStore{db: db, tenantID: tenant.MustParse("a")}
	ctx := tenant.Into(context.Background(), a.TenantID())
	ok, err := a.CompareDeliveryVerification(ctx, "delivery", 0, &types.DeliveryVerification{Revision: 1})
	if err != nil || !ok {
		t.Fatalf("own CAS: %v %v", ok, err)
	}
	if !reflect.DeepEqual(foreign, syncSnapshot(t, db, "b")) {
		t.Fatal("delivery CAS changed foreign notes/sync/FTS")
	}
	var raw string
	collisionMust(t, db.QueryRow("SELECT metadata FROM notes WHERE tenant_id='a' AND path='delivery'").Scan(&raw))
	wantJSON, err := json.Marshal(&types.DeliveryVerification{Revision: 1})
	collisionMust(t, err)
	var wantVerification interface{}
	collisionMust(t, json.Unmarshal(wantJSON, &wantVerification))
	want := map[string]interface{}{"unrelated": true, "delivery_verification": wantVerification}
	if !deliveryMetadataEqual(raw, want) {
		t.Fatalf("own metadata: got %s, want %+v", raw, want)
	}
	before := recoverySnapshot(t, db)
	ok, err = a.CompareDeliveryVerification(ctx, "delivery", 0, &types.DeliveryVerification{Revision: 2})
	if err != nil || ok {
		t.Fatalf("stale CAS: %v %v", ok, err)
	}
	if !reflect.DeepEqual(before, recoverySnapshot(t, db)) {
		t.Fatal("stale CAS changed database")
	}
	for _, bad := range []context.Context{context.Background(), tenant.Into(context.Background(), tenant.MustParse("b"))} {
		if _, err := a.CompareDeliveryVerification(bad, "delivery", 1, &types.DeliveryVerification{Revision: 2}); err == nil {
			t.Fatal("mismatched context admitted")
		}
	}
	if !reflect.DeepEqual(before, recoverySnapshot(t, db)) {
		t.Fatal("denied CAS changed database")
	}
}

// Extract the original oracle so negative controls prove it rejects plausible
// nonempty-but-wrong persisted metadata, not merely a missing value.
func deliveryMetadataEqual(raw string, want map[string]interface{}) bool {
	var got map[string]interface{}
	return json.Unmarshal([]byte(raw), &got) == nil && reflect.DeepEqual(got, want)
}

func TestDeliveryMetadataOracle(t *testing.T) {
	want := map[string]interface{}{"unrelated": true, "delivery_verification": map[string]interface{}{"revision": float64(1)}}
	for _, tc := range []struct {
		name, raw string
		valid     bool
	}{
		{"exact", `{"unrelated":true,"delivery_verification":{"revision":1}}`, true},
		{"wrong revision", `{"unrelated":true,"delivery_verification":{"revision":2}}`, false},
		{"lost unrelated", `{"delivery_verification":{"revision":1}}`, false},
		{"changed unrelated", `{"unrelated":false,"delivery_verification":{"revision":1}}`, false},
		{"missing verification", `{"unrelated":true}`, false},
		{"extra metadata", `{"unrelated":true,"extra":true,"delivery_verification":{"revision":1}}`, false},
		{"invalid JSON", `broken`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := deliveryMetadataEqual(tc.raw, want); got != tc.valid {
				t.Fatalf("metadata oracle accepted=%v, want %v: %s", got, tc.valid, tc.raw)
			}
		})
	}
}
