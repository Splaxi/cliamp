//go:build windows

package luaplugin

import "os"

// minimalExecEnv returns the restricted environment for plugin subprocesses:
// PATH, a HOME/USERPROFILE derived from homeEnv, plus the Windows variables
// subprocesses commonly need and any explicit cliamp config overrides so a
// `cliamp remote call` child resolves the same config dir as the daemon.
func minimalExecEnv() []string {
	home := homeEnv()
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"USERPROFILE=" + home,
	}
	// Pass through the Windows variables subprocesses commonly need; skip
	// any that are unset. CLIAMP_CONFIG_DIR/XDG_* must propagate so a
	// `cliamp remote call` child resolves the same config dir (and socket)
	// as the daemon instead of falling back to HOME/.config/cliamp.
	for _, key := range []string{"APPDATA", "LOCALAPPDATA", "CLIAMP_CONFIG_DIR", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "ComSpec", "PATHEXT", "SystemRoot", "WINDIR", "TEMP", "TMP"} {
		if v := os.Getenv(key); v != "" {
			env = append(env, key+"="+v)
		}
	}
	return env
}
