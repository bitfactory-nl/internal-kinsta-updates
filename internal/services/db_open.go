package services

import (
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

// openCommand is a test seam so tests never launch a real macOS application.
var openCommand = exec.Command

// BuildMySQLURL builds the mysql://-URL that Sequel Ace (and compatible
// tools) use to open a connection directly, without the user retyping a
// password. See https://sequel-ace.com/get-started/connect-via-url.html.
// net/url takes care of percent-encoding the user/password.
func BuildMySQLURL(host string, port int, user, password, dbName string) string {
	u := url.URL{
		Scheme: "mysql",
		User:   url.UserPassword(user, password),
		Host:   fmt.Sprintf("%s:%d", host, port),
		Path:   "/" + dbName,
	}
	return u.String()
}

// OpenInApp opens the given mysql://-URL in the named macOS application via
// `open -a`. appName is e.g. "Sequel Ace" or "TablePlus" — user-configurable
// in the settings.
func OpenInApp(appName, mysqlURL string) error {
	out, err := openCommand("open", "-a", appName, mysqlURL).CombinedOutput()
	if err != nil {
		return fmt.Errorf("kon %q niet openen: %w: %s", appName, err, strings.TrimSpace(string(out)))
	}
	return nil
}
