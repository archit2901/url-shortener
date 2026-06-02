package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrURLNotFound = errors.New("url not found")

// URL represents a row in the urls table.
type URL struct {
	ID        int64
	ShortCode *string // nullable until we set it
	LongURL   string
	CreatedAt time.Time
}

// URLRepository handles all database access for urls.
type URLRepository struct {
	pool *pgxpool.Pool
}

func NewURLRepository(pool *pgxpool.Pool) *URLRepository {
	return &URLRepository{pool: pool}
}

// Create inserts a new url row and returns it (with the auto-generated id).
func (r *URLRepository) Create(ctx context.Context, longURL string) (*URL, error) {
	query := `
		INSERT INTO urls (long_url)
		VALUES ($1)
		RETURNING id, short_code, long_url, created_at
	`
	var u URL
	err := r.pool.QueryRow(ctx, query, longURL).Scan(
		&u.ID,
		&u.ShortCode,
		&u.LongURL,
		&u.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// SetShortCode updates the short_code column for an existing url.
func (r *URLRepository) SetShortCode(ctx context.Context, id int64, code string) error {
	query := `UPDATE urls SET short_code = $1 WHERE id = $2`
	_, err := r.pool.Exec(ctx, query, code, id)
	return err
}

// GetByShortCode looks up a url by its short code.
func (r *URLRepository) GetByShortCode(ctx context.Context, code string) (*URL, error) {
	query := `
		SELECT id, short_code, long_url, created_at
		FROM urls
		WHERE short_code = $1
	`
	var u URL
	err := r.pool.QueryRow(ctx, query, code).Scan(
		&u.ID,
		&u.ShortCode,
		&u.LongURL,
		&u.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrURLNotFound
		}
		return nil, err
	}
	return &u, nil
}
