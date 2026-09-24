package appdir

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDir(t *testing.T) {
	tests := []struct {
		name        string
		env         map[string]string
		want        func(tempDir string) string
		windowsOnly bool
	}{
		{
			name: "home config",
			env:  map[string]string{"CLIAMP_CONFIG_DIR": "", "XDG_CONFIG_HOME": "", "APPDATA": "", "HOME": "TEMPDIR"},
			want: func(tmp string) string { return filepath.Join(tmp, ".config", "cliamp") },
		},
		{
			name: "xdg config",
			env:  map[string]string{"CLIAMP_CONFIG_DIR": "", "HOME": "", "APPDATA": "", "XDG_CONFIG_HOME": "TEMPDIR"},
			want: func(tmp string) string { return filepath.Join(tmp, "cliamp") },
		},
		{
			name:        "appdata on windows when home missing",
			windowsOnly: true,
			env:         map[string]string{"CLIAMP_CONFIG_DIR": "", "XDG_CONFIG_HOME": "", "HOME": "", "APPDATA": "TEMPDIR"},
			want:        func(tmp string) string { return filepath.Join(tmp, "cliamp") },
		},
		{
			// Plugin children receive a synthesized HOME (see
			// luaplugin/minimalExecEnv); on Windows they must still
			// resolve to APPDATA so they find the daemon socket.
			name:        "appdata on windows wins over home",
			windowsOnly: true,
			env:         map[string]string{"CLIAMP_CONFIG_DIR": "", "XDG_CONFIG_HOME": "", "HOME": "/some/home", "APPDATA": "TEMPDIR"},
			want:        func(tmp string) string { return filepath.Join(tmp, "cliamp") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.windowsOnly && runtime.GOOS != "windows" {
				t.Skip("Windows-specific fallback")
			}
			var tempDir string
			for k, v := range tt.env {
				if v == "TEMPDIR" {
					tempDir = t.TempDir()
					t.Setenv(k, tempDir)
				} else {
					t.Setenv(k, v)
				}
			}
			got, err := Dir()
			if err != nil {
				t.Fatalf("Dir() error: %v", err)
			}
			want := tt.want(tempDir)
			if got != want {
				t.Fatalf("Dir() = %q, want %q", got, want)
			}
		})
	}
}

// TestDirWindowsLegacyFallback covers the upgrade path: a Windows user whose
// config lives in the pre-fix HOME/.config/cliamp location keeps resolving
// there while APPDATA/cliamp has no config yet; once APPDATA has one, it wins.
func TestDirWindowsLegacyFallback(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific fallback")
	}
	appData := t.TempDir()
	home := t.TempDir()
	legacy := filepath.Join(home, ".config", "cliamp")
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "config.toml"), []byte("[plugins]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLIAMP_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("APPDATA", appData)
	t.Setenv("HOME", home)

	got, err := Dir()
	if err != nil {
		t.Fatalf("Dir() error: %v", err)
	}
	if got != legacy {
		t.Fatalf("Dir() = %q, want legacy %q", got, legacy)
	}

	// Once the canonical location has a config, it takes precedence.
	if err := os.MkdirAll(filepath.Join(appData, "cliamp"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appData, "cliamp", "config.toml"), []byte("[plugins]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = Dir()
	if err != nil {
		t.Fatalf("Dir() error: %v", err)
	}
	if want := filepath.Join(appData, "cliamp"); got != want {
		t.Fatalf("Dir() = %q, want %q", got, want)
	}
}

func TestPluginDir(t *testing.T) {
	t.Setenv("CLIAMP_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("APPDATA", "")
	t.Setenv("HOME", t.TempDir())

	dir, err := PluginDir()
	if err != nil {
		t.Fatalf("PluginDir() error: %v", err)
	}

	if !strings.HasSuffix(dir, filepath.Join("cliamp", "plugins")) {
		t.Fatalf("PluginDir() = %q, expected to end with cliamp/plugins", dir)
	}
}

func TestPluginDirIsSubdirOfDir(t *testing.T) {
	t.Setenv("CLIAMP_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("APPDATA", "")
	t.Setenv("HOME", t.TempDir())

	base, _ := Dir()
	plugin, _ := PluginDir()

	if !strings.HasPrefix(plugin, base) {
		t.Fatalf("PluginDir %q should be under Dir %q", plugin, base)
	}
}
