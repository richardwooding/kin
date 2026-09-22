package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func polite(t *testing.T, h http.HandlerFunc) (*Polite, string) {
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	p := NewPolite("test")
	p.Delay = 0
	p.Backoff = []time.Duration{0, 0}
	p.HTTP = srv.Client()
	return p, srv.URL
}

func TestPoliteRetriesThrottleThenGivesUp(t *testing.T) {
	n := 0
	p, u := polite(t, func(w http.ResponseWriter, r *http.Request) {
		n++
		if !strings.HasPrefix(r.UserAgent(), "kin/") || r.Header.Get("Accept") != "application/json" {
			t.Errorf("headers: %q %q", r.UserAgent(), r.Header.Get("Accept"))
		}
		if n < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	if b, err := p.Get(context.Background(), u, "application/json"); err != nil || string(b) != "ok" {
		t.Fatalf("two 429s are retried: %q %v", b, err)
	}
	n = -10
	if _, err := p.Get(context.Background(), u+"/again", "application/json"); err == nil || !strings.HasPrefix(err.Error(), "test: 429") {
		t.Errorf("the schedule spent, the 429 is reported: %v", err)
	}
}

func TestPoliteDoesNotRetryOtherErrors(t *testing.T) {
	n := 0
	p, u := polite(t, func(w http.ResponseWriter, _ *http.Request) {
		n++
		w.WriteHeader(http.StatusNotFound)
	})
	if _, err := p.Get(context.Background(), u, ""); err == nil || n != 1 {
		t.Errorf("a 404 is asked once and reported: %d %v", n, err)
	}
}

func TestPoliteReadsTheCache(t *testing.T) {
	n := 0
	p, u := polite(t, func(w http.ResponseWriter, _ *http.Request) {
		n++
		_, _ = w.Write([]byte("fresh"))
	})
	p.Cache = NewCache(t.TempDir())
	b, _ := p.Get(context.Background(), u, "")
	p.Cache.Put(u, b)
	if b, err := p.Get(context.Background(), u, ""); err != nil || string(b) != "fresh" || n != 1 {
		t.Errorf("the second read comes from the cache: %q %v, %d requests", b, err, n)
	}
}

func TestSleepStopsWithTheContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Sleep(ctx, time.Hour); err == nil {
		t.Error("a cancelled context ends the wait")
	}
}
