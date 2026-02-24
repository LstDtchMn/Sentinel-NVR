package detection

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/LstDtchMn/Sentinel-NVR/backend/pkg/dbutil"
)

// FaceRecord represents a row from the faces table (Phase 13, R11).
type FaceRecord struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Thumbnail string    `json:"thumbnail"` // forward-slash-normalized path
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Embedding is never exposed via JSON — used internally for matching only.
	Embedding []float32 `json:"-"`
}

// FaceRepository provides CRUD access to the faces table.
type FaceRepository struct {
	db *sql.DB
}

// NewFaceRepository creates a face repository.
func NewFaceRepository(db *sql.DB) *FaceRepository {
	return &FaceRepository{db: db}
}

// Create inserts a new face with its embedding and returns the created record.
func (r *FaceRepository) Create(ctx context.Context, name string, embedding []float32, thumbnail string) (*FaceRecord, error) {
	blob := encodeEmbedding(embedding)
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO faces (name, embedding, thumbnail) VALUES (?, ?, ?)`,
		name, blob, thumbnail,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting face: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("getting face ID: %w", err)
	}
	return r.GetByID(ctx, int(id))
}

// GetByID returns a face by ID. Returns ErrNotFound if absent.
func (r *FaceRepository) GetByID(ctx context.Context, id int) (*FaceRecord, error) {
	var f FaceRecord
	var blob []byte
	var createdStr, updatedStr string
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, embedding, thumbnail, created_at, updated_at FROM faces WHERE id = ?`, id,
	).Scan(&f.ID, &f.Name, &blob, &f.Thumbnail, &createdStr, &updatedStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("getting face %d: %w", id, err)
	}
	f.Embedding = decodeEmbedding(blob)
	f.CreatedAt, err = dbutil.ParseSQLiteTime(createdStr)
	if err != nil {
		return nil, fmt.Errorf("parsing created_at for face %d: %w", id, err)
	}
	f.UpdatedAt, err = dbutil.ParseSQLiteTime(updatedStr)
	if err != nil {
		return nil, fmt.Errorf("parsing updated_at for face %d: %w", id, err)
	}
	return &f, nil
}

// List returns all enrolled faces (without embeddings in JSON — only IDs and names).
func (r *FaceRepository) List(ctx context.Context) ([]FaceRecord, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, thumbnail, created_at, updated_at FROM faces ORDER BY name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing faces: %w", err)
	}
	defer rows.Close()

	var faces []FaceRecord
	for rows.Next() {
		var f FaceRecord
		var createdStr, updatedStr string
		if err := rows.Scan(&f.ID, &f.Name, &f.Thumbnail, &createdStr, &updatedStr); err != nil {
			return nil, fmt.Errorf("scanning face row: %w", err)
		}
		if f.CreatedAt, err = dbutil.ParseSQLiteTime(createdStr); err != nil {
			return nil, fmt.Errorf("parsing created_at for face %d: %w", f.ID, err)
		}
		if f.UpdatedAt, err = dbutil.ParseSQLiteTime(updatedStr); err != nil {
			return nil, fmt.Errorf("parsing updated_at for face %d: %w", f.ID, err)
		}
		faces = append(faces, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating face rows: %w", err)
	}
	if faces == nil {
		faces = []FaceRecord{} // return empty array, not null
	}
	return faces, nil
}

// ListWithEmbeddings returns all enrolled faces including their embeddings.
// Used internally by the face matching service — not exposed via API.
func (r *FaceRepository) ListWithEmbeddings(ctx context.Context) ([]FaceRecord, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, embedding, thumbnail, created_at, updated_at FROM faces ORDER BY name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing faces with embeddings: %w", err)
	}
	defer rows.Close()

	var faces []FaceRecord
	for rows.Next() {
		var f FaceRecord
		var blob []byte
		var createdStr, updatedStr string
		if err := rows.Scan(&f.ID, &f.Name, &blob, &f.Thumbnail, &createdStr, &updatedStr); err != nil {
			return nil, fmt.Errorf("scanning face row: %w", err)
		}
		f.Embedding = decodeEmbedding(blob)
		if f.CreatedAt, err = dbutil.ParseSQLiteTime(createdStr); err != nil {
			return nil, fmt.Errorf("parsing created_at for face %d: %w", f.ID, err)
		}
		if f.UpdatedAt, err = dbutil.ParseSQLiteTime(updatedStr); err != nil {
			return nil, fmt.Errorf("parsing updated_at for face %d: %w", f.ID, err)
		}
		faces = append(faces, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating face rows: %w", err)
	}
	if faces == nil {
		faces = []FaceRecord{}
	}
	return faces, nil
}

// Update renames a face. Does not change the embedding.
func (r *FaceRepository) Update(ctx context.Context, id int, name string) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE faces SET name = ?, updated_at = ? WHERE id = ?`,
		name, time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("updating face %d: %w", id, err)
	}
	affected, rowsErr := result.RowsAffected()
	if rowsErr != nil {
		return fmt.Errorf("checking rows affected: %w", rowsErr)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes a face by ID.
func (r *FaceRepository) Delete(ctx context.Context, id int) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM faces WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting face %d: %w", id, err)
	}
	affected, rowsErr := result.RowsAffected()
	if rowsErr != nil {
		return fmt.Errorf("checking rows affected: %w", rowsErr)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// MatchFace compares an embedding against all enrolled faces and returns the best
// match above the threshold. Returns (nil, nil) if no match meets the threshold.
func (r *FaceRepository) MatchFace(ctx context.Context, embedding []float32, threshold float64) (*FaceRecord, float64, error) {
	faces, err := r.ListWithEmbeddings(ctx)
	if err != nil {
		return nil, 0, err
	}
	if len(faces) == 0 {
		return nil, 0, nil
	}

	var bestFace *FaceRecord
	bestSimilarity := 0.0
	for i := range faces {
		sim := cosineSimilarity(embedding, faces[i].Embedding)
		if sim > bestSimilarity {
			bestSimilarity = sim
			bestFace = &faces[i]
		}
	}
	if bestFace == nil || bestSimilarity < threshold {
		return nil, bestSimilarity, nil
	}
	return bestFace, bestSimilarity, nil
}

// cosineSimilarity computes the cosine similarity between two float32 vectors.
// Returns a value in [-1.0, 1.0]; higher = more similar.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		ai, bi := float64(a[i]), float64(b[i])
		dot += ai * bi
		normA += ai * ai
		normB += bi * bi
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}

// encodeEmbedding converts a float32 slice to a little-endian byte slice for BLOB storage.
func encodeEmbedding(embedding []float32) []byte {
	buf := make([]byte, len(embedding)*4)
	for i, v := range embedding {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

// decodeEmbedding converts a BLOB back to a float32 slice.
func decodeEmbedding(blob []byte) []float32 {
	n := len(blob) / 4
	result := make([]float32, n)
	for i := 0; i < n; i++ {
		result[i] = math.Float32frombits(binary.LittleEndian.Uint32(blob[i*4:]))
	}
	return result
}
