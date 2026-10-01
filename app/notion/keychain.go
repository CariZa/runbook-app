package notion

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
)

// The integration token lives in the login Keychain, never in a file in the library, so it
// isn't in runbook.md, in exports or in backups of ~/runbooks.
//
// The item is stored with -A (no per-application prompt). Per-app ACLs would be granted to
// /usr/bin/security, which any process can run, so they'd cost a prompt on every read
// without actually restricting access. Anything running as this user can therefore read the
// token, exactly as with a file in the home directory; the Keychain adds encryption at rest
// and keeps it out of the library.
const (
	keychainService = "runbook-notion-token"
	keychainAccount = "runbook"
)

// ErrNoToken means no Notion token has been saved yet.
var ErrNoToken = errors.New("no Notion token saved")

// SaveToken stores (or replaces) the integration token in the login Keychain.
func SaveToken(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("paste your Notion integration token")
	}
	cmd := exec.Command("security", "add-generic-password",
		"-a", keychainAccount, "-s", keychainService, "-w", token, "-U", "-A",
		"-D", "application password", "-j", "Runbook app: Notion integration token")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if strings.Contains(msg, "canceled") {
			return errors.New("the macOS Keychain prompt was dismissed; try Connect again and allow it")
		}
		return errors.New("couldn't save to the Keychain: " + msg)
	}
	return nil
}

// Token reads the saved token, or ErrNoToken.
func Token() (string, error) {
	out, err := exec.Command("security", "find-generic-password",
		"-a", keychainAccount, "-s", keychainService, "-w").Output()
	if err != nil {
		return "", ErrNoToken
	}
	token := strings.TrimSpace(string(out))
	if token == "" {
		return "", ErrNoToken
	}
	return token, nil
}

// ForgetToken removes the token from the Keychain.
func ForgetToken() error {
	cmd := exec.Command("security", "delete-generic-password", "-a", keychainAccount, "-s", keychainService)
	if err := cmd.Run(); err != nil {
		return ErrNoToken
	}
	return nil
}

// Hint is the saved token, masked: enough to tell one token from another, never enough to
// use. It returns "" when no token is saved.
func Hint() string {
	tok, err := Token()
	if err != nil {
		return ""
	}
	return hint(tok)
}

func hint(tok string) string {
	r := []rune(tok)
	if len(r) <= 10 {
		return strings.Repeat("\u2022", len(r))
	}
	return string(r[:4]) + strings.Repeat("\u2022", 6) + string(r[len(r)-4:])
}
