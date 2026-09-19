package spotify

import (
	"testing"

	playlist4pb "github.com/devgianlu/go-librespot/proto/spotify/playlist4"
	"google.golang.org/protobuf/proto"
)

func rootlistContent(uris []string, metas []*playlist4pb.MetaItem) *playlist4pb.SelectedListContent {
	items := make([]*playlist4pb.Item, len(uris))
	for i, u := range uris {
		items[i] = &playlist4pb.Item{Uri: proto.String(u)}
	}
	return &playlist4pb.SelectedListContent{
		Contents: &playlist4pb.ListItems{Items: items, MetaItems: metas},
	}
}

func meta(name string, length int32, owner string) *playlist4pb.MetaItem {
	return &playlist4pb.MetaItem{
		Attributes:    &playlist4pb.ListAttributes{Name: proto.String(name)},
		Length:        proto.Int32(length),
		OwnerUsername: proto.String(owner),
	}
}

// A response carrying playlists but no metadata at all cannot be named, and
// unnamed entries are hidden -- so it must fail into the Web API rather than
// present an empty library as if that were the truth.
func TestParseRootlistRejectsMissingMetadata(t *testing.T) {
	content := rootlistContent([]string{"spotify:playlist:a", "spotify:playlist:b"}, nil)
	if _, err := parseRootlist(content); err == nil {
		t.Fatal("parsed a metadata-less response, want an error so the Web API serves the list")
	}
}

func TestParseRootlistEmptyLibraryIsNotAnError(t *testing.T) {
	got, err := parseRootlist(rootlistContent(nil, nil))
	if err != nil {
		t.Fatalf("empty library: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d entries, want 0", len(got))
	}
}

func TestParseRootlistReadsFoldersAndPlaylists(t *testing.T) {
	content := rootlistContent(
		[]string{
			"spotify:start-group:f1:Late+Night+%26+Chill",
			"spotify:playlist:inner",
			"spotify:end-group:f1",
			"spotify:playlist:outer",
		},
		[]*playlist4pb.MetaItem{nil, meta("Inner", 3, "listener"), nil, meta("Outer", 7, "listener")},
	)
	got, err := parseRootlist(content)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d entries, want 4", len(got))
	}
	if got[0].Name != "Late Night & Chill" || !got[0].FolderOpen {
		t.Errorf("folder start = %+v, want the decoded name and FolderOpen", got[0])
	}
	if got[1].Name != "Inner" || got[1].TrackCount != 3 {
		t.Errorf("inner playlist = %+v", got[1])
	}
	if !got[2].isFolder() || got[2].FolderOpen {
		t.Errorf("folder end = %+v, want a close boundary", got[2])
	}
}

// Folder sections come from the open-group stack. An end-group naming a folder
// that is not the innermost one must not silently reparent what follows it.
func TestRootlistPlaylistsIgnoresMismatchedEndGroup(t *testing.T) {
	entries := []rootlistEntry{
		{URI: "spotify:start-group:f1:Outer", Name: "Outer", FolderID: "f1", FolderOpen: true},
		{URI: "spotify:start-group:f2:Inner", Name: "Inner", FolderID: "f2", FolderOpen: true},
		{URI: "spotify:playlist:a", Name: "Nested", TrackCount: 1, Owner: "listener"},
		{URI: "spotify:end-group:f1", FolderID: "f1"}, // closes the outer one out of order
		{URI: "spotify:playlist:b", Name: "After", TrackCount: 1, Owner: "listener"},
	}
	p := &SpotifyProvider{}
	got := p.rootlistPlaylists(entries, "listener")
	if len(got) != 2 {
		t.Fatalf("got %d playlists, want 2", len(got))
	}
	if got[0].Section != "Outer / Inner" {
		t.Errorf("nested playlist section = %q, want %q", got[0].Section, "Outer / Inner")
	}
	if got[1].Section != "Outer / Inner" {
		t.Errorf("section after a mismatched end-group = %q, want the stack left intact", got[1].Section)
	}
}
