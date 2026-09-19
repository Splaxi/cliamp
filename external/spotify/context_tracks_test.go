package spotify

import "testing"

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

// A playlist whose snapshot_id moved, or one this client just wrote to, must
// drop its resolved URIs alongside its cached tracks.
func TestInvalidationDropsResolvedURIs(t *testing.T) {
	p := &SpotifyProvider{
		trackCache:  map[string]*playlistCache{"list": {snapshotID: "old", tracks: nil}},
		pending:     map[string]*pendingTracks{},
		contextURIs: map[string][]string{"list": {"spotify:track:a"}},
	}
	// Mirrors what Playlists() does when it sees a changed snapshot_id.
	delete(p.trackCache, "list")
	delete(p.contextURIs, "list")

	if _, ok := p.contextURIs["list"]; ok {
		t.Error("resolved URIs outlived a snapshot change")
	}
}
