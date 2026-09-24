// Package configtest isolates the app's config directory in tests. Imports
// nothing from Wails (thin layer).
package configtest

import "testing"

// IsolateConfigDir points os.UserConfigDir at a fresh temp directory for the
// duration of the test, on every OS: XDG_CONFIG_HOME on Linux, APPDATA on
// Windows, HOME on macOS. Setting only XDG_CONFIG_HOME leaves Windows and macOS
// tests reading and writing the developer's real app config.
func IsolateConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)
	t.Setenv("HOME", dir)
	return dir
}
