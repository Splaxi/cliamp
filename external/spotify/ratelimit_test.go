package spotify

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/bjarneo/cliamp/playlist"
)

// Spotify extends a cooldown when it is asked again during one: a second
// becomes a minute becomes a day. A long hold must therefore be reported, not
// waited out, and the caller told how long rather than left to time out.
func TestRateLimitIsReportedNotWaitedOut(t *testing.T) {
	for _, tc := range []struct {
		name          string
		retryAfter    string
		wantRequests  int
		wantRateLimit bool
	}{
		{name: "long hold fails at once", retryAfter: "3287", wantRequests: 1, wantRateLimit: true},
		{name: "beyond the cap fails at once", retryAfter: "61", wantRequests: 1, wantRateLimit: true},
		{name: "short hold is retried then given up", retryAfter: "1", wantRequests: 3, wantRateLimit: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			original := http.DefaultTransport
			http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				requests++
				h := make(http.Header)
				h.Set("Retry-After", tc.retryAfter)
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Status:     "429 Too Many Requests",
					Header:     h,
					Body:       io.NopCloser(strings.NewReader(`{"error":{"status":429}}`)),
					Request:    req,
				}, nil
			})
			t.Cleanup(func() { http.DefaultTransport = original })

			sess := &Session{tokenSource: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "t"})}
			p := New(sess, "client", 320)

			start := time.Now()
			_, err := p.webAPI(t.Context(), "GET", "/v1/me", nil)
			elapsed := time.Since(start)

			if err == nil {
				t.Fatal("a 429 was reported as success")
			}
			if tc.wantRateLimit && !errors.Is(err, playlist.ErrRateLimited) {
				t.Errorf("error %v does not identify itself as a rate limit", err)
			}
			if requests != tc.wantRequests {
				t.Errorf("made %d requests, want %d", requests, tc.wantRequests)
			}
			// A long hold must not be slept through; the old code waited the
			// full Retry-After inside a context that could never outlast it.
			if elapsed > 10*time.Second {
				t.Errorf("waited %v; a cooldown should be reported, not slept through", elapsed)
			}
			fmt.Printf("  %-28s requests=%d elapsed=%v err=%v\n", tc.name, requests, elapsed.Round(time.Millisecond), err)
		})
	}
}

// A refusal applies to the whole list, not one page. Asking again for every
// later page costs a request per page and keeps pressure on an API that is
// already refusing, which is how a cooldown gets extended.
func TestWebAPIIsAskedOncePerRead(t *testing.T) {
	webCalls, clientCalls := 0, 0
	original := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasPrefix(req.URL.Path, "/v1/") {
			webCalls++
			h := make(http.Header)
			h.Set("Retry-After", "3287")
			return &http.Response{StatusCode: http.StatusTooManyRequests, Status: "429 Too Many Requests",
				Header: h, Body: io.NopCloser(strings.NewReader(`{}`)), Request: req}, nil
		}
		clientCalls++
		return nil, fmt.Errorf("no client session in this test")
	})
	t.Cleanup(func() { http.DefaultTransport = original })

	sess := &Session{tokenSource: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "t"})}
	p := New(sess, "client", 320)

	for offset := 0; offset < 300; offset += spotifyTrackPageSize {
		_, _ = p.fetchTracksPage(t.Context(), "somelist", offset, nil)
	}
	if webCalls > 1 {
		t.Errorf("asked the web api %d times for one read; it refused on the first", webCalls)
	}
}
