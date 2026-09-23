package appdir

import (
	"os"
	"path/filepath"
	"runtime"
)

// Dir returns the cliamp configuration directory.
//
// Resolution order:
//   - CLIAMP_CONFIG_DIR (explicit override)
//   - XDG_CONFIG_HOME/cliamp
//   - on Windows: APPDATA/cliamp (preferred over HOME, which Git Bash/MSYS
//     set to %USERPROFILE% and would otherwise split the config dir between
//     the daemon and plugin children that receive a synthesized HOME),
//     with a fallback to the legacy HOME/.config/cliamp location when the
//     APPDATA location has no config yet but the legacy one does
//   - HOME/.config/cliamp
//   - fallback: os.UserHomeDir()/.config/cliamp
func Dir() (string, error) {
	if dir, ok := os.LookupEnv("CLIAMP_CONFIG_DIR"); ok && dir != "" {
		return dir, nil
	}
	if xdg, ok := os.LookupEnv("XDG_CONFIG_HOME"); ok && xdg != "" {
		return filepath.Join(xdg, "cliamp"), nil
	}
	if runtime.GOOS == "windows" {
		if appData, ok := os.LookupEnv("APPDATA"); ok && appData != "" {
			appDir := filepath.Join(appData, "cliamp")
			if _, err := os.Stat(filepath.Join(appDir, "config.toml")); err != nil {
				if legacy := legacyWindowsDir(); legacy != "" && legacy != appDir {
					if _, lerr := os.Stat(filepath.Join(legacy, "config.toml")); lerr == nil {
						return legacy, nil
					}
				}
			}
			return appDir, nil
		}
	}
	if home, ok := os.LookupEnv("HOME"); ok && home != "" {
		return filepath.Join(home, ".config", "cliamp"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "cliamp"), nil
}

// legacyWindowsDir returns the pre-fix Windows config location
// (HOME/.config/cliamp, or os.UserHomeDir()/.config/cliamp when HOME is
// unset) so Dir can fall back to it on upgrade. Returns "" when neither
// HOME nor a home directory is available.
func legacyWindowsDir() string {
	if home, ok := os.LookupEnv("HOME"); ok && home != "" {
		return filepath.Join(home, ".config", "cliamp")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".config", "cliamp")
	}
	return ""
}

// PluginDir returns the cliamp plugin directory.
func PluginDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "plugins"), nil
}

// DataDir returns the cliamp data directory (~/.local/share/cliamp), used for
// state that is not user-edited config: plugin stores, downloaded assets, etc.
func DataDir() (string, error) {
	// Honor HOME first, matching Dir(); on Windows os.UserHomeDir() reads
	// USERPROFILE and ignores HOME, so this keeps the two resolvers consistent.
	if home, ok := os.LookupEnv("HOME"); ok && home != "" {
		return filepath.Join(home, ".local", "share", "cliamp"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "cliamp"), nil
}
