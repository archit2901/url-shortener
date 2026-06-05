package repository

import (
	"context"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ClickStats struct {
	TotalClicks    int64
	UniqueVisitors int64
	ClicksByDay    []DailyClicks
}

type DailyClicks struct {
	Day    time.Time
	Clicks int64
}

type ClickStatsRepository struct {
	pool *pgxpool.Pool
}

func NewClickStatsRepository(pool *pgxpool.Pool) *ClickStatsRepository {
	return &ClickStatsRepository{pool: pool}
}

func (r *ClickStatsRepository) GetStatsForURL(ctx context.Context, urlID int64) (*ClickStats, error) {
	span := sentry.StartSpan(ctx, "db.query", sentry.WithDescription("compute click stats"))
	defer span.Finish()

	stats := &ClickStats{}

	summarySpan := sentry.StartSpan(ctx, "db.query", sentry.WithDescription("SELECT total + unique"))
	summaryQuery := `
		SELECT COUNT(*) AS total,
		       COUNT(DISTINCT ip_hash) FILTER (WHERE ip_hash <> '') AS unique_visitors
		FROM clicks
		WHERE url_id = $1
	`
	err := r.pool.QueryRow(ctx, summaryQuery, urlID).Scan(&stats.TotalClicks, &stats.UniqueVisitors)
	summarySpan.Finish()
	if err != nil {
		return nil, err
	}

	dailySpan := sentry.StartSpan(ctx, "db.query", sentry.WithDescription("SELECT clicks by day"))
	dailyQuery := `
		SELECT DATE_TRUNC('day', clicked_at) AS day,
		       COUNT(*) AS clicks
		FROM clicks
		WHERE url_id = $1
		  AND clicked_at >= NOW() - INTERVAL '30 days'
		GROUP BY day
		ORDER BY day ASC
	`
	rows, err := r.pool.Query(ctx, dailyQuery, urlID)
	dailySpan.Finish()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var d DailyClicks
		if err := rows.Scan(&d.Day, &d.Clicks); err != nil {
			return nil, err
		}
		stats.ClicksByDay = append(stats.ClicksByDay, d)
	}
	return stats, rows.Err()
}
