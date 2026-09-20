package spotify

import "testing"

// The switch has broken before. web must never reach the client protocol, and
// client must never fall back to the Web API, or the escape hatch the docs
// promise is not the one the code provides.
func TestResolveAPIMode(t *testing.T) {
	for _, tc := range []struct {
		env        string
		want       apiMode
		usesClient bool
		skipsWeb   bool
		wantString string
	}{
		{"", apiModeAuto, true, false, "auto"},
		{"auto", apiModeAuto, true, false, "auto"},
		{"client", apiModeClient, true, true, "client"},
		{"CLIENT", apiModeClient, true, true, "client"},
		{"  client  ", apiModeClient, true, true, "client"},
		{"web", apiModeWeb, false, false, "web"},
		{"WEB", apiModeWeb, false, false, "web"},
		{"nonsense", apiModeAuto, true, false, "auto"},
	} {
		t.Setenv(apiModeEnv, tc.env)
		got := resolveAPIMode()
		if got != tc.want {
			t.Errorf("%q: mode = %v, want %v", tc.env, got, tc.want)
		}
		if got.usesClient() != tc.usesClient {
			t.Errorf("%q: usesClient = %v, want %v", tc.env, got.usesClient(), tc.usesClient)
		}
		if got.skipsWeb() != tc.skipsWeb {
			t.Errorf("%q: skipsWeb = %v, want %v", tc.env, got.skipsWeb(), tc.skipsWeb)
		}
		if got.String() != tc.wantString {
			t.Errorf("%q: String = %q, want %q", tc.env, got.String(), tc.wantString)
		}
	}
}

// web reproduces the behaviour from before the client protocol existed, and
// client never quietly borrows the Web API for a read it can serve itself.
func TestAPIModeReadPaths(t *testing.T) {
	if apiModeWeb.usesClient() {
		t.Error("web mode would use the client protocol")
	}
	if apiModeAuto.skipsWeb() {
		t.Error("auto mode would skip the Web API, so nothing could fall back")
	}
	if !apiModeClient.skipsWeb() {
		t.Error("client mode would fall back to the Web API, masking a broken endpoint")
	}
}
