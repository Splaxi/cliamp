package spotify

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bjarneo/cliamp/playlist"
	"golang.org/x/oauth2"
)

// stubAddTrack accepts any POST to a playlist's items.
func stubAddTrack(t *testing.T, calls *int) *SpotifyProvider {
	t.Helper()
	original := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		*calls++
		return &http.Response{
			StatusCode: http.StatusCreated,
			Status:     "201 Created",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"snapshot_id":"new"}`)),
			Request:    req,
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = original })
	sess := &Session{tokenSource: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "token"})}
	return New(sess, "client", 320)
}

// The resolved URI list is a snapshot of the playlist taken when a read began.
// Keeping it past the read would serve an edited playlist from stale contents,
// and the snapshot pin could not tell, because the length it compares comes
// from that same stale list.
func TestResolvedURIsDoNotOutliveTheRead(t *testing.T) {
	p := &SpotifyProvider{
		trackCache:  map[string]*playlistCache{},
		pending:     map[string]*pendingTracks{},
		contextURIs: map[string][]string{},
	}

	for _, name := range []string{"committed", "discarded"} {
		t.Run(name, func(t *testing.T) {
			p.pending["list"] = &pendingTracks{want: 50, total: 100}
			p.contextURIs["list"] = []string{"spotify:track:a", "spotify:track:b"}

			p.discardLoadLocked("list")

			if _, ok := p.contextURIs["list"]; ok {
				t.Error("resolved URIs survived the read; a later reopen would serve stale contents")
			}
			if _, ok := p.pending["list"]; ok {
				t.Error("partial accumulation survived the read")
			}
		})
	}
}

// A playlist this client just wrote to must drop its resolved URIs alongside
// its cached tracks, or the next read would slice a list taken before the
// write. Driven through the real write path rather than a copy of it.
func TestInvalidationDropsResolvedURIs(t *testing.T) {
	calls := 0
	p := stubAddTrack(t, &calls)
	p.mu.Lock()
	p.trackCache["list"] = &playlistCache{snapshotID: "old", tracks: []playlist.Track{{Path: "spotify:track:a"}}}
	p.contextURIs["list"] = []string{"spotify:track:a"}
	p.mu.Unlock()

	if err := p.AddTrackToPlaylist(context.Background(), "list", playlist.Track{Path: "spotify:track:b"}); err != nil {
		t.Fatalf("AddTrackToPlaylist: %v", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.contextURIs["list"]; ok {
		t.Error("resolved URIs outlived a write to the playlist")
	}
	if _, ok := p.trackCache["list"]; ok {
		t.Error("cached tracks outlived a write to the playlist")
	}
}
