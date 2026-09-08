package storage_test

import (
	"reflect"
	"testing"

	"github.com/huynle/brain-api/internal/indexer"
	"github.com/huynle/brain-api/internal/storage"
)

func TestNoRawDBAccessors(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeOf(storage.StorageLayer{}),
		reflect.TypeOf(storage.TenantStore{}),
		reflect.TypeOf(indexer.Indexer{}),
	} {
		t.Run(typ.Name(), func(t *testing.T) {
			for _, methodSet := range []reflect.Type{typ, reflect.PointerTo(typ)} {
				if _, ok := methodSet.MethodByName("DB"); ok {
					t.Errorf("%v exposes raw DB accessor", methodSet)
				}
			}
		})
	}
}
