package observability

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/getsentry/sentry-go"
)

// InitSentry configures and initializes the Sentry SDK using environment variables.
// Returns a flush function that should be deferred in main.
func InitSentry(release string) (func(), error) {
	dsn := os.Getenv("SENTRY_DSN")
	if dsn == "" {
		// No DSN configured: return a no-op flush. This lets the app run
		// without Sentry (e.g. in tests) without crashing.
		return func() {}, nil
	}

	environment := os.Getenv("SENTRY_ENVIRONMENT")
	if environment == "" {
		environment = "development"
	}

	tracesRate := 1.0
	if rate := os.Getenv("SENTRY_TRACES_SAMPLE_RATE"); rate != "" {
		if parsed, err := strconv.ParseFloat(rate, 64); err == nil {
			tracesRate = parsed
		}
	}

	err := sentry.Init(sentry.ClientOptions{
		Dsn:              dsn,
		Environment:      environment,
		Release:          release,
		TracesSampleRate: tracesRate,
		EnableTracing:    true,
		// Send PII-stripped request data
		SendDefaultPII: false,
		// Scrub sensitive fields before transmitting
		BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			// Strip query strings — they can contain tokens/sensitive data
			if event.Request != nil {
				event.Request.QueryString = ""
			}
			return event
		},
	})
	if err != nil {
		return nil, fmt.Errorf("sentry init failed: %w", err)
	}

	// Returns a flush function so main can ensure all events are sent before exit
	return func() {
		sentry.Flush(2 * time.Second)
	}, nil
}
