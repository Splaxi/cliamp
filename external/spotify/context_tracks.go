package spotify

import (
	"context"
	"fmt"
	"strings"

	extmetapb "github.com/devgianlu/go-librespot/proto/spotify/extendedmetadata"
	metadatapb "github.com/devgianlu/go-librespot/proto/spotify/metadata"
	playerpb "github.com/devgianlu/go-librespot/proto/spotify/player"

	"github.com/bjarneo/cliamp/playlist"
)

// Reading a playlist through Spotify's client protocol takes two steps: the
// context resolves to an ordered list of track URIs, then metadata for those
// URIs is fetched in batches. It is the only way to read playlists the Web API
// refuses -- another user's list, or a Spotify-owned mix -- and it is not
// subject to the Web API's quota.

// contextTrackURIs returns every track URI in a playlist, in order, caching the
// result so paging through it costs one request rather than one per page.
func (p *SpotifyProvider) contextTrackURIs(ctx context.Context, playlistID string) ([]string, error) {
	p.mu.Lock()
	if uris, ok := p.contextURIs[playlistID]; ok {
		p.mu.Unlock()
		return uris, nil
	}
	p.mu.Unlock()

	sess := p.session
	if sess == nil || sess.sess == nil {
		return nil, fmt.Errorf("spotify: context resolve: no session")
	}
	content, err := sess.sess.Spclient().ContextResolve(ctx, "spotify:playlist:"+playlistID)
	if err != nil {
		return nil, fmt.Errorf("spotify: context resolve %q: %w", playlistID, err)
	}

	var uris []string
	for _, page := range content.GetPages() {
		for _, tr := range page.GetTracks() {
			if uri := tr.GetUri(); strings.HasPrefix(uri, "spotify:track:") {
				uris = append(uris, uri)
			}
		}
	}

	p.mu.Lock()
	p.contextURIs[playlistID] = uris
	p.mu.Unlock()
	return uris, nil
}

// trackMetadata resolves a batch of track URIs to full tracks in one request.
// Entries Spotify declines to describe are skipped rather than failing the
// batch, so one unavailable track cannot cost the whole page.
func (p *SpotifyProvider) trackMetadata(ctx context.Context, uris []string) ([]playlist.Track, error) {
	if len(uris) == 0 {
		return nil, nil
	}
	sess := p.session
	if sess == nil || sess.sess == nil {
		return nil, fmt.Errorf("spotify: track metadata: no session")
	}

	reqs := make([]*extmetapb.EntityRequest, 0, len(uris))
	for _, uri := range uris {
		reqs = append(reqs, &extmetapb.EntityRequest{
			EntityUri: uri,
			Query:     []*extmetapb.ExtensionQuery{{ExtensionKind: extmetapb.ExtensionKind_TRACK_V4}},
		})
	}
	res, err := sess.sess.Spclient().ExtendedMetadata(ctx, &extmetapb.BatchedEntityRequest{EntityRequest: reqs})
	if err != nil {
		return nil, fmt.Errorf("spotify: track metadata: %w", err)
	}

	// The response is not ordered like the request, so index it and rebuild the
	// page in the order the playlist actually holds.
	byURI := make(map[string]playlist.Track, len(uris))
	for _, ext := range res.GetExtendedMetadata() {
		for _, d := range ext.GetExtensionData() {
			var tr metadatapb.Track
			if err := d.GetExtensionData().UnmarshalTo(&tr); err != nil {
				continue
			}
			byURI[d.GetEntityUri()] = trackFromMetadata(d.GetEntityUri(), &tr)
		}
	}

	out := make([]playlist.Track, 0, len(uris))
	for _, uri := range uris {
		if t, ok := byURI[uri]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}

// trackFromMetadata converts Spotify's internal track message into a playlist
// entry. Duration arrives in milliseconds and artists as a list, matching how
// trackFromItem treats the Web API's shape.
func trackFromMetadata(uri string, tr *metadatapb.Track) playlist.Track {
	names := make([]string, 0, len(tr.GetArtist()))
	for _, a := range tr.GetArtist() {
		if n := a.GetName(); n != "" {
			names = append(names, n)
		}
	}
	return playlist.Track{
		Path:         uri,
		Title:        tr.GetName(),
		Artist:       strings.Join(names, ", "),
		Album:        tr.GetAlbum().GetName(),
		DurationSecs: int(tr.GetDuration()) / 1000,
		TrackNumber:  int(tr.GetNumber()),
		Stream:       false,
	}
}

// contextTracksPage serves one page of a playlist through the client protocol,
// returning the page and the playlist's total length.
func (p *SpotifyProvider) contextTracksPage(ctx context.Context, playlistID string, offset int) ([]playlist.Track, int, error) {
	uris, err := p.contextTrackURIs(ctx, playlistID)
	if err != nil {
		return nil, 0, err
	}
	if offset >= len(uris) {
		return nil, len(uris), nil
	}
	end := min(offset+spotifyTrackPageSize, len(uris))
	tracks, err := p.trackMetadata(ctx, uris[offset:end])
	if err != nil {
		return nil, 0, err
	}
	return tracks, len(uris), nil
}

// TrackRadio builds the station Spotify generates from a track -- what its own
// clients call song radio. The station resolves to track URIs like any other
// context, so the metadata is filled in the same way.
//
// Implements provider.RadioStarter.
func (p *SpotifyProvider) TrackRadio(ctx context.Context, trackPath string) (string, []playlist.Track, error) {
	if err := p.ensureSession(); err != nil {
		return "", nil, err
	}
	if !strings.HasPrefix(trackPath, "spotify:track:") {
		return "", nil, fmt.Errorf("spotify: radio: %q is not a spotify track", trackPath)
	}
	sess := p.session
	if sess == nil || sess.sess == nil {
		return "", nil, fmt.Errorf("spotify: radio: no session")
	}

	uri := trackPath
	station, err := sess.sess.Spclient().ContextResolveAutoplay(ctx, &playerpb.AutoplayContextRequest{ContextUri: &uri})
	if err != nil {
		return "", nil, fmt.Errorf("spotify: radio for %q: %w", trackPath, err)
	}

	var uris []string
	for _, page := range station.GetPages() {
		for _, tr := range page.GetTracks() {
			if u := tr.GetUri(); strings.HasPrefix(u, "spotify:track:") {
				uris = append(uris, u)
			}
		}
	}
	if len(uris) == 0 {
		return "", nil, fmt.Errorf("spotify: radio for %q: station is empty", trackPath)
	}

	// Stations come back around fifty tracks long, which is one metadata batch.
	// Resolve in page-sized chunks anyway so a longer one cannot build a single
	// oversized request.
	tracks := make([]playlist.Track, 0, len(uris))
	for start := 0; start < len(uris); start += spotifyTrackPageSize {
		end := min(start+spotifyTrackPageSize, len(uris))
		batch, err := p.trackMetadata(ctx, uris[start:end])
		if err != nil {
			return "", nil, err
		}
		tracks = append(tracks, batch...)
	}
	return station.GetUri(), tracks, nil
}
