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
			// An explicitly customized HOME keeps working as before, so
			// existing tests (which point HOME at a temp dir) and users
			// who overrode HOME are unaffected by the APPDATA preference.
			name:        "custom home on windows wins over appdata",
			windowsOnly: true,
			env:         map[string]string{"CLIAMP_CONFIG_DIR": "", "XDG_CONFIG_HOME": "", "HOME": "/some/custom/home", "APPDATA": "TEMPDIR"},
			want:        func(tmp string) string { return filepath.Join("/some/custom/home", ".config", "cliamp") },
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

// TestResolveWindowsDir covers the Windows selection logic directly (pure
// function, runs on every OS): a default HOME loses to APPDATA, a customized
// HOME falls through, and the legacy location is used on upgrade while the
// APPDATA location has no config yet.
func TestResolveWindowsDir(t *testing.T) {
	writeConfig := func(t *testing.T, dir string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("[plugins]\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("default home resolves to appdata", func(t *testing.T) {
		appData := t.TempDir()
		userHome := t.TempDir()
		got, ok := resolveWindowsDir(appData, userHome, true, userHome)
		if !ok {
			t.Fatal("expected ok=true for a default HOME")
		}
		if want := filepath.Join(appData, "cliamp"); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("unset home resolves to appdata", func(t *testing.T) {
		appData := t.TempDir()
		got, ok := resolveWindowsDir(appData, "", false, t.TempDir())
		if !ok {
			t.Fatal("expected ok=true for an unset HOME")
		}
		if want := filepath.Join(appData, "cliamp"); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("custom home falls through", func(t *testing.T) {
		got, ok := resolveWindowsDir(t.TempDir(), t.TempDir(), true, t.TempDir())
		if ok {
			t.Fatalf("expected ok=false for a customized HOME, got %q", got)
		}
	})

	t.Run("legacy fallback while appdata has no config", func(t *testing.T) {
		appData := t.TempDir()
		userHome := t.TempDir()
		legacy := filepath.Join(userHome, ".config", "cliamp")
		writeConfig(t, legacy)
		got, ok := resolveWindowsDir(appData, userHome, true, userHome)
		if !ok {
			t.Fatal("expected ok=true for the legacy fallback")
		}
		if got != legacy {
			t.Fatalf("got %q, want legacy %q", got, legacy)
		}
	})

	t.Run("appdata wins once it has a config", func(t *testing.T) {
		appData := t.TempDir()
		userHome := t.TempDir()
		writeConfig(t, filepath.Join(userHome, ".config", "cliamp"))
		writeConfig(t, filepath.Join(appData, "cliamp"))
		got, ok := resolveWindowsDir(appData, userHome, true, userHome)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if want := filepath.Join(appData, "cliamp"); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("fresh install resolves to appdata", func(t *testing.T) {
		appData := t.TempDir()
		userHome := t.TempDir()
		got, ok := resolveWindowsDir(appData, userHome, true, userHome)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if want := filepath.Join(appData, "cliamp"); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}

// TestDirWindowsDefaultHomeUsesAppdata checks Dir end to end for the case
// that matters in production: HOME mirrors the profile dir (Git Bash/MSYS
// default, or the HOME synthesized for plugin children), so APPDATA wins.
// The APPDATA config is created so the legacy fallback cannot interfere.
func TestDirWindowsDefaultHomeUsesAppdata(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific resolution")
	}
	userHome, err := os.UserHomeDir()
	if err != nil || userHome == "" {
		t.Skip("no home directory available")
	}
	appData := t.TempDir()
	appDir := filepath.Join(appData, "cliamp")
	if err := os.MkdirAll(appDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "config.toml"), []byte("[plugins]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLIAMP_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("APPDATA", appData)
	t.Setenv("HOME", userHome)

	got, err := Dir()
	if err != nil {
		t.Fatalf("Dir() error: %v", err)
	}
	if got != appDir {
		t.Fatalf("Dir() = %q, want %q", got, appDir)
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
