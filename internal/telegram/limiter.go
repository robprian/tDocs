package telegram

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gotd/td/tgerr"
)

// SafeLimiter coordinates single-worker upload concurrency, pacing, and flood wait backoff.
type SafeLimiter struct {
	uploadMu    sync.Mutex
	pacingDelay time.Duration
}

func NewSafeLimiter() *SafeLimiter {
	return &SafeLimiter{
		pacingDelay: 30 * time.Millisecond,
	}
}

// AcquireUpload locks the single-worker upload mutex to prevent concurrent uploads in Safe Mode.
func (l *SafeLimiter) AcquireUpload() func() {
	l.uploadMu.Lock()
	return func() {
		l.uploadMu.Unlock()
	}
}

// Pace introduces a slight human-like delay between consecutive MTProto parts.
func (l *SafeLimiter) Pace(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(l.pacingDelay):
		return nil
	}
}

// ExecuteWithFloodWait executes an MTProto call, sleeping if Telegram returns FLOOD_WAIT_X.
func (l *SafeLimiter) ExecuteWithFloodWait(ctx context.Context, fn func() error) error {
	const maxRetries = 3
	for attempt := 0; attempt < maxRetries; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}

		if d, ok := tgerr.AsFloodWait(err); ok {
			cooldown := d + 5*time.Second
			fmt.Printf("⚠️ Telegram FLOOD_WAIT encountered: pausing for %v...\n", cooldown)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(cooldown):
				continue
			}
		}

		return err
	}
	return fmt.Errorf("exceeded maximum retries during flood wait backoff")
}
