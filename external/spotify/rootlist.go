package spotify

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strings"
	"time"

	playlist4pb "github.com/devgianlu/go-librespot/proto/spotify/playlist4"
	"google.golang.org/protobuf/proto"

	"github.com/bjarneo/cliamp/applog"
	"github.com/bjarneo/cliamp/playlist"
)

// Spotify keeps the library as an ordered list with folders marked inline: a
// start-group entry opens one and an end-group entry with the same id closes
// it, and they nest. The Web API has no concept of folders and omits entries it
// will not serve, so the rootlist is both richer and cheaper -- one request for
// the whole library rather than one per fifty playlists.
const rootlistEnrichBudget = 10 * time.Second

const (
	rootlistGroupStart = "spotify:start-group:"
	rootlistGroupEnd   = "spotify:end-group:"
	rootlistPlaylist   = "spotify:playlist:"
)

// rootlistEntry is one row of the library: a playlist, or a folder boundary.
type rootlistEntry struct {
	URI        string
	Name       string
	TrackCount int
	Owner      string
	FolderID   string // set for boundaries
	FolderOpen bool   // true for start-group, false for end-group
}

// isFolder reports whether the entry opens or closes a folder rather than
// naming a playlist.
func (e rootlistEntry) isFolder() bool { return e.FolderID != "" }

// rootlist reads the library through Spotify's own client protocol. It returns
// entries in the order Spotify stores them, folder boundaries included.
func (p *SpotifyProvider) rootlist(ctx context.Context) ([]rootlistEntry, error) {
	sess := p.session
	if sess == nil || sess.sess == nil {
		return nil, fmt.Errorf("spotify: rootlist: no session")
	}
	user := sess.sess.Username()
	if user == "" {
		return nil, fmt.Errorf("spotify: rootlist: no username")
	}

	hm := fmt.Sprintf("hm://playlist/v2/user/%s/rootlist?decorate=revision,length,attributes,timestamp,owner", user)
	resp, err := sess.sess.Spclient().RequestHm(ctx, "GET", hm, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("spotify: rootlist: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("spotify: rootlist: http status %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("spotify: rootlist: read: %w", err)
	}

	var content playlist4pb.SelectedListContent
	if err := proto.Unmarshal(body, &content); err != nil {
		return nil, fmt.Errorf("spotify: rootlist: parse: %w", err)
	}

	items := content.GetContents().GetItems()
	// meta_items runs parallel to items and carries the name, track count and
	// owner. It can be short or absent, so index defensively.
	metas := content.GetContents().GetMetaItems()

	entries := make([]rootlistEntry, 0, len(items))
	for i, item := range items {
		uri := item.GetUri()
		switch {
		case strings.HasPrefix(uri, rootlistGroupStart):
			id, name := parseGroupStart(uri)
			entries = append(entries, rootlistEntry{URI: uri, Name: name, FolderID: id, FolderOpen: true})
		case strings.HasPrefix(uri, rootlistGroupEnd):
			entries = append(entries, rootlistEntry{
				URI:      uri,
				FolderID: strings.TrimPrefix(uri, rootlistGroupEnd),
			})
		case strings.HasPrefix(uri, rootlistPlaylist):
			e := rootlistEntry{URI: uri}
			if i < len(metas) {
				m := metas[i]
				e.Name = m.GetAttributes().GetName()
				e.TrackCount = int(m.GetLength())
				e.Owner = m.GetOwnerUsername()
			}
			entries = append(entries, e)
		}
	}
	return entries, nil
}

// parseGroupStart splits a start-group URI into its id and display name. The
// name is URL-encoded with spaces as "+", so "My+love+%3C3" is "My love <3".
func parseGroupStart(uri string) (id, name string) {
	rest := strings.TrimPrefix(uri, rootlistGroupStart)
	id, encoded, found := strings.Cut(rest, ":")
	if !found {
		return rest, ""
	}
	decoded, err := url.QueryUnescape(encoded)
	if err != nil {
		decoded = strings.ReplaceAll(encoded, "+", " ")
	}
	return id, decoded
}

// playlistIDFromURI returns the bare id of a spotify:playlist: URI.
func playlistIDFromURI(uri string) string {
	return strings.TrimPrefix(uri, rootlistPlaylist)
}

// playlistsFromRootlist builds the library rows from Spotify's own client
// protocol, prepending Liked Songs and appending saved albums so the result
// matches what the Web API path produces.
func (p *SpotifyProvider) playlistsFromRootlist(ctx context.Context) ([]playlist.PlaylistInfo, error) {
	entries, err := p.rootlist(ctx)
	if err != nil {
		return nil, err
	}

	lists := make([]playlist.PlaylistInfo, 0, len(entries)+8)

	// Liked Songs and saved albums still come from the Web API, which can be
	// throttled independently of the client protocol. Neither is worth losing
	// the whole library over, so each gets a short deadline of its own and is
	// skipped on failure rather than failing the listing.
	if liked, err := p.savedTracksInfo(withBudget(ctx)); err == nil {
		lists = append(lists, liked)
	} else {
		applog.Warn("spotify: liked songs count unavailable: %v", err)
		lists = append(lists, playlist.PlaylistInfo{
			ID:      savedTracksPlaylistID,
			Name:    "Your Music",
			Section: "Library",
		})
	}

	lists = append(lists, p.rootlistPlaylists(entries, p.session.sess.Username())...)

	if albums, err := p.savedAlbums(withBudget(ctx)); err == nil {
		lists = append(lists, albums...)
	} else {
		applog.Warn("spotify: saved albums unavailable: %v", err)
	}

	p.mu.Lock()
	p.rememberRootlistLocked(entries)
	p.mu.Unlock()
	return lists, nil
}

// withBudget bounds an optional enrichment so a throttled Web API cannot spend
// the caller's whole deadline on something the listing can do without.
func withBudget(ctx context.Context) context.Context {
	out, cancel := context.WithTimeout(ctx, rootlistEnrichBudget)
	// The caller returns before the budget elapses in every path here; cancel
	// on a timer so the context is not leaked if that ever stops being true.
	time.AfterFunc(rootlistEnrichBudget, cancel)
	return out
}

// rememberRootlistLocked keeps the ordered library, folders included, so a
// later tree view can render it without another request. p.mu must be held.
func (p *SpotifyProvider) rememberRootlistLocked(entries []rootlistEntry) {
	p.rootlistEntries = slices.Clone(entries)
}

// rootlistPlaylists converts library entries into the provider's playlist rows,
// skipping folder boundaries. Ownership decides the section, matching what the
// Web API path reports.
func (p *SpotifyProvider) rootlistPlaylists(entries []rootlistEntry, userID string) []playlist.PlaylistInfo {
	lists := make([]playlist.PlaylistInfo, 0, len(entries))
	for _, e := range entries {
		if e.isFolder() {
			continue
		}
		section := "Followed playlists"
		if userID != "" && e.Owner == userID {
			section = "Your playlists"
		}
		lists = append(lists, playlist.PlaylistInfo{
			ID:         playlistIDFromURI(e.URI),
			Name:       e.Name,
			TrackCount: e.TrackCount,
			Section:    section,
		})
	}
	return lists
}
