package collections

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"

	"fluxport/internal/conversion"
)

// A scalar source id is a business identity. Otherwise identical values retain
// their identity across reordering; occurrences distinguish duplicate records.
func identityKey(row any, payload []byte) string {
	if object, ok := row.(map[string]any); ok {
		if id, exists := object["id"]; exists && id != nil {
			switch id.(type) {
			case string, bool, float64, int, int64:
				b, _ := conversion.MarshalJSON(id)
				return "id:" + string(b)
			default:
				// JSON numbers are preserved as json.Number, not float64.
				b, _ := conversion.MarshalJSON(id)
				if len(b) > 0 && b[0] != '{' && b[0] != '[' {
					return "id:" + string(b)
				}
			}
		}
	}
	h := sha256.Sum256(payload)
	return "value:" + hex.EncodeToString(h[:])
}

func recordID(collection, key string, occurrence int64) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", collection, key, occurrence)))
	return hex.EncodeToString(h[:16])
}

func (s *Service) applyRecordOverrides(ctx context.Context, path, collection, digest string) error {
	var count int64
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM record_overrides WHERE collection_id=? AND source_sha256=?`, collection, digest).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT ordinal,record_id FROM record_overrides WHERE collection_id=? AND source_sha256=? ORDER BY ordinal`, collection, digest)
	if err != nil {
		return err
	}
	defer rows.Close()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var indexed int64
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM records`).Scan(&indexed); err != nil {
		return err
	}
	if indexed != count {
		return fmt.Errorf("record identity mapping does not match source")
	}
	// Remove generated IDs first so reorder/delete overrides cannot conflict.
	if _, err = tx.ExecContext(ctx, `UPDATE records SET record_id='temporary-'||ordinal`); err != nil {
		return err
	}
	for rows.Next() {
		var ordinal int64
		var id string
		if err = rows.Scan(&ordinal, &id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE records SET record_id=? WHERE ordinal=?`, id, ordinal); err != nil {
			return err
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	return tx.Commit()
}
