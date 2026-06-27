package promptr

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"
)

// Registry maps the client names declared in a .promptr file to live Providers
// the caller has wired up. Generated client constructors resolve their provider
// references through a Registry, so the DSL stays free of credentials.
type Registry map[string]Provider

// Get returns the Provider registered under name, or an error Provider that
// fails every call — so a missing wiring surfaces at call time with a clear
// message rather than a nil-pointer panic.
func (r Registry) Get(name string) Provider {
	if p, ok := r[name]; ok && p != nil {
		return p
	}
	return errProvider{name}
}

type errProvider struct{ name string }

func (e errProvider) Complete(context.Context, []Message) (string, error) {
	return "", fmt.Errorf("promptr: no provider registered for client %q", e.name)
}

// retryProvider re-attempts the wrapped provider on error.
type retryProvider struct {
	inner    Provider
	attempts int
	backoff  time.Duration
}

// Retry wraps p so that Complete is retried up to attempts times on error,
// sleeping backoff between tries (respecting context cancellation). attempts < 1
// is treated as 1. This is reliability against transient failures, distinct from
// Extract's parse-repair loop (which re-asks on unparseable but successful
// replies).
func Retry(p Provider, attempts int, backoff time.Duration) Provider {
	if attempts < 1 {
		attempts = 1
	}
	return retryProvider{inner: p, attempts: attempts, backoff: backoff}
}

func (r retryProvider) Complete(ctx context.Context, msgs []Message) (string, error) {
	var lastErr error
	for i := 0; i < r.attempts; i++ {
		if i > 0 && r.backoff > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(r.backoff):
			}
		}
		out, err := r.inner.Complete(ctx, msgs)
		if err == nil {
			return out, nil
		}
		lastErr = err
	}
	return "", lastErr
}

// fallbackProvider tries each provider in order until one succeeds.
type fallbackProvider struct{ providers []Provider }

// Fallback wraps providers so Complete tries each in order, returning the first
// success; if all fail, the last error is returned. Use it to fail over from a
// primary model to a backup.
func Fallback(providers ...Provider) Provider {
	return fallbackProvider{providers: providers}
}

func (f fallbackProvider) Complete(ctx context.Context, msgs []Message) (string, error) {
	if len(f.providers) == 0 {
		return "", fmt.Errorf("promptr: Fallback has no providers")
	}
	var lastErr error
	for _, p := range f.providers {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		out, err := p.Complete(ctx, msgs)
		if err == nil {
			return out, nil
		}
		lastErr = err
	}
	return "", lastErr
}

// roundRobinProvider spreads calls across providers in rotation.
type roundRobinProvider struct {
	providers []Provider
	n         atomic.Uint64
}

// RoundRobin wraps providers so each Complete call goes to the next provider in
// rotation — load-spreading across keys or endpoints. It is safe for concurrent
// use.
func RoundRobin(providers ...Provider) Provider {
	return &roundRobinProvider{providers: providers}
}

func (r *roundRobinProvider) Complete(ctx context.Context, msgs []Message) (string, error) {
	if len(r.providers) == 0 {
		return "", fmt.Errorf("promptr: RoundRobin has no providers")
	}
	i := r.n.Add(1) - 1
	return r.providers[int(i%uint64(len(r.providers)))].Complete(ctx, msgs)
}
