package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrURLNotFound = errors.New("url not found")

type URL struct {
	ID        int64
	ShortCode *string
	LongURL   string
	UserID    *uuid.UUID
	CreatedAt time.Time
}

type URLRepository struct {
	pool *pgxpool.Pool
}

func NewURLRepository(pool *pgxpool.Pool) *URLRepository {
	return &URLRepository{pool: pool}
}

// Create inserts a new url row. userID may be nil for anonymous shortenings.
func (r *URLRepository) Create(ctx context.Context, longURL string, userID *uuid.UUID) (*URL, error) {
	query := `
		INSERT INTO urls (long_url, user_id)
		VALUES ($1, $2)
		RETURNING id, short_code, long_url, user_id, created_at
	`
	var u URL
	err := r.pool.QueryRow(ctx, query, longURL, userID).Scan(
		&u.ID, &u.ShortCode, &u.LongURL, &u.UserID, &u.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *URLRepository) SetShortCode(ctx context.Context, id int64, code string) error {
	query := `UPDATE urls SET short_code = $1 WHERE id = $2`
	_, err := r.pool.Exec(ctx, query, code, id)
	return err
}

func (r *URLRepository) GetByShortCode(ctx context.Context, code string) (*URL, error) {
	query := `
		SELECT id, short_code, long_url, user_id, created_at
		FROM urls
		WHERE short_code = $1
	`
	var u URL
	err := r.pool.QueryRow(ctx, query, code).Scan(
		&u.ID, &u.ShortCode, &u.LongURL, &u.UserID, &u.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrURLNotFound
		}
		return nil, err
	}
	return &u, nil
}

// URLWithStats is a URL plus its aggregate click count.
type URLWithStats struct {
	URL
	ClickCount int64
}

// ListByUserID returns all URLs owned by a user with aggregated click counts,
// newest first.
func (r *URLRepository) ListByUserID(ctx context.Context, userID uuid.UUID, limit, offset int) ([]URLWithStats, error) {
	query := `
		SELECT u.id, u.short_code, u.long_url, u.user_id, u.created_at,
		       COALESCE(c.click_count, 0) AS click_count
		FROM urls u
		LEFT JOIN (
			SELECT url_id, COUNT(*) AS click_count
			FROM clicks
			GROUP BY url_id
		) c ON c.url_id = u.id
		WHERE u.user_id = $1
		ORDER BY u.created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.pool.Query(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []URLWithStats
	for rows.Next() {
		var u URLWithStats
		if err := rows.Scan(
			&u.ID, &u.ShortCode, &u.LongURL, &u.UserID, &u.CreatedAt, &u.ClickCount,
		); err != nil {
			return nil, err
		}
		results = append(results, u)
	}
	return results, rows.Err()
}
