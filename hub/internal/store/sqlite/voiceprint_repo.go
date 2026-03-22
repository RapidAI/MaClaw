package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"math"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// encodeEmbedding converts []float32 to little-endian bytes for BLOB storage.
func encodeEmbedding(emb []float32) []byte {
	buf := make([]byte, len(emb)*4)
	for i, v := range emb {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

// decodeEmbedding converts little-endian BLOB back to []float32.
func decodeEmbedding(data []byte) []float32 {
	if len(data)%4 != 0 {
		return nil
	}
	n := len(data) / 4
	emb := make([]float32, n)
	for i := 0; i < n; i++ {
		emb[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return emb
}

func (r *voiceprintRepo) Create(ctx context.Context, vp *store.Voiceprint) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO voiceprints (id, user_id, email, label, embedding, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		vp.ID, vp.UserID, vp.Email, vp.Label,
		encodeEmbedding(vp.Embedding),
		vp.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	)
	return err
}

func (r *voiceprintRepo) ListByUserID(ctx context.Context, userID string) ([]*store.Voiceprint, error) {
	rows, err := r.readDB.QueryContext(ctx,
		`SELECT id, user_id, email, label, embedding, created_at FROM voiceprints WHERE user_id = ? ORDER BY created_at`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanVoiceprints(rows)
}

func (r *voiceprintRepo) ListAll(ctx context.Context) ([]*store.Voiceprint, error) {
	rows, err := r.readDB.QueryContext(ctx,
		`SELECT id, user_id, email, label, embedding, created_at FROM voiceprints ORDER BY email, created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanVoiceprints(rows)
}

func (r *voiceprintRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM voiceprints WHERE id = ?`, id)
	return err
}

func (r *voiceprintRepo) DeleteByUserID(ctx context.Context, userID string) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM voiceprints WHERE user_id = ?`, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func scanVoiceprints(rows *sql.Rows) ([]*store.Voiceprint, error) {
	var result []*store.Voiceprint
	for rows.Next() {
		var vp store.Voiceprint
		var embBlob []byte
		var createdAt string
		if err := rows.Scan(&vp.ID, &vp.UserID, &vp.Email, &vp.Label, &embBlob, &createdAt); err != nil {
			return nil, err
		}
		vp.Embedding = decodeEmbedding(embBlob)
		vp.CreatedAt = mustParseTime(createdAt)
		result = append(result, &vp)
	}
	return result, rows.Err()
}
