package httpx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// DefaultBackoff is how long a client waits before each retry of a throttled
// or dropped request.
var DefaultBackoff = []time.Duration{15 * time.Second, 45 * time.Second, 90 * time.Second, 180 * time.Second}

// Polite fetches from a rate-limited service: answers come from the cache
// when it holds them, every request that reaches the service is followed by
// Delay, and a 429, a 503 or a dropped connection is retried after each
// Backoff wait in turn, or the server's Retry-After when that is longer.
// Callers Put an answer in the Cache once they have decoded it, so a garbled
// page is fetched again next time.
type Polite struct {
	Name    string // prefixes errors, e.g. "riksarkivet"
	HTTP    *http.Client
	Delay   time.Duration
	Backoff []time.Duration
	Cache   *Cache
}

// NewPolite returns a fetcher with a 60 s timeout, a one-second delay and
// DefaultBackoff.
func NewPolite(name string) *Polite {
	return &Polite{Name: name, HTTP: &http.Client{Timeout: 60 * time.Second}, Delay: time.Second, Backoff: DefaultBackoff}
}

var errRetry = errors.New("retry")

// Get reads u, sending the kin user agent and the given Accept header.
func (p *Polite) Get(ctx context.Context, u, accept string) ([]byte, error) {
	if b, ok := p.Cache.Get(u); ok {
		return b, nil
	}
	for attempt := 0; ; attempt++ {
		b, wait, err := p.get(ctx, u, accept)
		if p.Delay > 0 {
			if err := Sleep(ctx, p.Delay); err != nil {
				return nil, err
			}
		}
		if !errors.Is(err, errRetry) || attempt >= len(p.Backoff) {
			return b, err
		}
		if wait < p.Backoff[attempt] {
			wait = p.Backoff[attempt]
		}
		if err := Sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
}

func (p *Polite) get(ctx context.Context, u, accept string) (body []byte, wait time.Duration, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", UserAgent())
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := p.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, ctx.Err()
		}
		return nil, 0, fmt.Errorf("%s: %v: %w", p.Name, err, errRetry)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	switch {
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable:
		secs, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		return nil, time.Duration(secs) * time.Second, fmt.Errorf("%s: %s: %w", p.Name, resp.Status, errRetry)
	case resp.StatusCode != http.StatusOK:
		return nil, 0, fmt.Errorf("%s: %s", p.Name, resp.Status)
	case err != nil:
		return nil, 0, fmt.Errorf("%s: %v: %w", p.Name, err, errRetry)
	}
	return b, 0, nil
}

// Sleep waits d, or until ctx is done.
func Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
