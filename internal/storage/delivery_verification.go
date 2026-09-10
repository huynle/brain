package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/huynle/brain-api/internal/types"
)

// CompareDeliveryVerification atomically replaces one versioned state object.
// It cannot lose unrelated metadata or resurrect stale evidence after a new head.
func (s *TenantStore) CompareDeliveryVerification(ctx context.Context, path string, expected int, value *types.DeliveryVerification) (bool, error) {
	if _, err := s.bulkScope(ctx); err != nil {
		return false, err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return false, err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE notes SET metadata=json_set(CASE WHEN json_type(metadata)='object' THEN metadata ELSE '{}' END,'$.delivery_verification',json(?)) WHERE path=? AND COALESCE(json_extract(metadata,'$.delivery_verification.revision'),0)=?`, string(data), path, expected)
	if err != nil {
		return false, fmt.Errorf("update delivery evidence: %w", err)
	}
	n, err := result.RowsAffected()
	return n == 1, err
}
