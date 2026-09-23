//go:build windows

package luaplugin

import "os"

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
