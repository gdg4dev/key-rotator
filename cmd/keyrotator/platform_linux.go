//go:build linux

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// XDG autostart is honoured by GNOME, KDE, XFCE, Cinnamon and most other
// desktops. Headless servers without a desktop session can use a systemd user
// unit instead; see the README.
const autostartMechanism = "XDG autostart (~/.config/autostart)"

func desktopEntryPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "autostart", "keyrotator.desktop"), nil
}

func autostartEnabled() bool {
	p, err := desktopEntryPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

func autostartApply(enable bool, args []string) error {
	p, err := desktopEntryPath()
	if err != nil {
		return err
	}
	if !enable {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	argv := []string{exe}
	argv = append(argv, args...)
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = desktopQuote(a)
	}
	entry := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=KeyRotator
Comment=Local API key rotation proxy
Exec=%s
Terminal=false
X-GNOME-Autostart-enabled=true
`, strings.Join(quoted, " "))

	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(entry), 0o644)
}

// desktopQuote applies the Desktop Entry Exec quoting rules: arguments with
// special characters are double-quoted, and "%" is doubled so it is not read
// as a field code.
func desktopQuote(s string) string {
	s = strings.ReplaceAll(s, "%", "%%")
	if !strings.ContainsAny(s, " \t\"`$\\><~|&;*?#()'") {
		return s
	}
	r := strings.NewReplacer(`"`, `\"`, "`", "\\`", "$", `\$`, `\`, `\\`)
	return `"` + r.Replace(s) + `"`
}

func openBrowser(url string) {
	_ = exec.Command("xdg-open", url).Start()
}
