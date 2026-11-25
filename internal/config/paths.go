package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DefaultPath returns the configuration file location:
// $XDG_CONFIG_HOME/lazyobs/config.yaml, ~/.config/lazyobs/config.yaml on
// Unix-like systems, %AppData%\lazyobs\config.yaml on Windows.
func DefaultPath(getenv func(string) string) string {
	if dir := getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "lazyobs", "config.yaml")
	}
	if runtime.GOOS == "windows" {
		if dir, err := os.UserConfigDir(); err == nil {
			return filepath.Join(dir, "lazyobs", "config.yaml")
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "lazyobs", "config.yaml")
	}
	return filepath.Join(home, ".config", "lazyobs", "config.yaml")
}

// Path returns the configuration file to use: the --config flag, then
// $LAZYOBS_CONFIG, then DefaultPath.
func Path(o Overrides, getenv func(string) string) string {
	return ExpandHome(firstNonEmpty(o.ConfigPath, getenv(EnvConfig), DefaultPath(getenv)))
}

// ExpandHome replaces a leading "~" with the user's home directory.
func ExpandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") && !strings.HasPrefix(path, `~\`) {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}

// tooOpen reports whether a file holding a secret is readable by others.
// File modes are not meaningful on Windows, which uses ACLs.
func tooOpen(path string) (bool, os.FileMode) {
	if runtime.GOOS == "windows" {
		return false, 0
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, 0
	}
	mode := info.Mode().Perm()
	return mode&0o077 != 0, mode
}
