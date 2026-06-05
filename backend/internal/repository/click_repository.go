package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Click represents a single redirect event.
type Click struct {
	URLID     int64
	IPHash    string
	UserAgent string
	Referrer  string
	ClickedAt time.Time
}

// ClickRepository handles database access for the clicks table.
type ClickRepository struct {
	pool *pgxpool.Pool
}

func NewClickRepository(pool *pgxpool.Pool) *ClickRepository {
	return &ClickRepository{pool: pool}
}

// InsertBatch inserts many clicks in a single bulk operation.
// Returns the number of rows inserted.
func (r *ClickRepository) InsertBatch(ctx context.Context, clicks []Click) (int, error) {
	if len(clicks) == 0 {
		return 0, nil
	}

	// pgx's CopyFrom uses Postgres's COPY protocol, which is the fastest
	// way to bulk-insert. Much faster than multi-row INSERT for non-trivial
	// batches (~hundreds of rows or more).
	rowsCopied, err := r.pool.CopyFrom(
		ctx,
		pgx.Identifier{"clicks"},
		[]string{"url_id", "ip_hash", "user_agent", "referrer", "clicked_at"},
		newClickRows(clicks),
	)
	if err != nil {
		return 0, err
	}
	return int(rowsCopied), nil
}

// clickRows is a pgx.CopyFromSource that streams clicks one at a time.
type clickRows struct {
	idx    int
	clicks []Click
}

func newClickRows(clicks []Click) *clickRows {
	return &clickRows{idx: -1, clicks: clicks}
}

func (c *clickRows) Next() bool {
	c.idx++
	return c.idx < len(c.clicks)
}

func (c *clickRows) Values() ([]any, error) {
	cl := c.clicks[c.idx]
	return []any{cl.URLID, cl.IPHash, cl.UserAgent, cl.Referrer, cl.ClickedAt}, nil
}

func (c *clickRows) Err() error {
	return nil
}
