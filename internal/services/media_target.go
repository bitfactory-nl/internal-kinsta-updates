package services

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	sshadapter "github.com/rdm/sites-tool/internal/adapters/ssh"
	"github.com/rdm/sites-tool/internal/domain"
)

// mediaPHPBudget is the analyzer's own wall-clock budget in seconds. It sits well
// under mediaScanTimeout so a slow site returns a marked partial result instead of
// being killed mid-scan. Generous on purpose: an incomplete scan cannot answer the
// question it was asked, so waiting is better than truncating.
const mediaPHPBudget = 1200

// mediaTarget is everything one scan needs: where to connect and where the site
// lives on the server.
type mediaTarget struct {
	SSH     sshadapter.Target
	Webroot string // leeg = op de server zoeken
	EnvName string
}

// mediaSSHTarget builds the SSH target from the project's own config plus the
// endpoint the Kinsta API reports. The username cannot be derived from the API, so
// it has to come from .rdm.yml; the password arrives already resolved from the
// keychain, and is empty when the login uses a key.
func mediaSSHTarget(p domain.Project, ep EnvSSHEndpoint, wachtwoord string) (mediaTarget, error) {
	var user, pad string
	if p.Config.SSH != nil {
		user = strings.TrimSpace(p.Config.SSH.User)
		pad = strings.TrimSpace(p.Config.SSH.Path)
	}
	if user == "" {
		return mediaTarget{}, fmt.Errorf("geen SSH-gebruiker bekend voor %s; vul die eenmalig in (te vinden in MyKinsta onder SFTP/SSH)", p.DisplayName)
	}
	if ep.Host == "" || ep.Port == 0 {
		return mediaTarget{}, fmt.Errorf("geen SSH-adres van Kinsta voor deze omgeving")
	}
	return mediaTarget{
		SSH:     sshadapter.Target{Host: ep.Host, Port: ep.Port, User: user, Password: wachtwoord},
		Webroot: pad,
		EnvName: ep.EnvName,
	}, nil
}

// shellQuote wraps a value in single quotes so a path can never break out of the
// command, however odd its characters.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// zoekWebroot is the shell snippet that locates the WordPress root. Kinsta puts it
// in ~/public, which mirrors the local `<project>/public` convention.
func zoekWebroot(webroot string) string {
	if webroot != "" {
		return "root=" + shellQuote(webroot)
	}
	return `root=""; for d in "$HOME/public" "$HOME/www" "$HOME/htdocs" "$HOME"; do if [ -f "$d/wp-config.php" ]; then root="$d"; break; fi; done`
}

// buildMediaScanCommand returns the whole scan as one shell command: every
// RunCommand is a fresh SSH dial, so splitting this up would cost a handshake per
// step. The analyzer is piped in over stdin, which leaves no file on the server.
func buildMediaScanCommand(webroot, script string, folders []string) string {
	env := fmt.Sprintf("RDM_MEDIA_BUDGET=%d", mediaPHPBudget)
	if len(folders) > 0 {
		// Base64 zodat mapnamen met spaties of aanhalingstekens de shell niet raken.
		env += " RDM_MEDIA_FOLDERS=" + base64.StdEncoding.EncodeToString([]byte(strings.Join(folders, "\n")))
	}
	return strings.Join([]string{
		zoekWebroot(webroot),
		`if [ -z "$root" ] || [ ! -f "$root/wp-config.php" ]; then echo "RDM-ERR:geen wp-config.php gevonden"; exit 3; fi`,
		`cd "$root" || exit 3`,
		`echo "RDM-ROOT:$root"`,
		`echo "RDM-DU:$(du -sk wp-content/uploads 2>/dev/null | cut -f1)"`,
		fmt.Sprintf(`printf %%s '%s' | base64 -d | %s nice -n 19 wp eval-file - 2>&1`,
			base64.StdEncoding.EncodeToString([]byte(script)), env),
	}, "\n")
}

// buildMediaProbeCommand collects, in one round trip, everything needed to judge
// whether a scan can work at all: which user we are, where the site is, whether
// WP-CLI answers, and how big uploads is.
func buildMediaProbeCommand(webroot string) string {
	return strings.Join([]string{
		`echo "RDM-USER:$(id -un)"`,
		`echo "RDM-HOME:$HOME"`,
		zoekWebroot(webroot),
		`echo "RDM-ROOT:$root"`,
		`if [ -n "$root" ]; then cd "$root" || exit 3; fi`,
		`echo "RDM-WPCLI:$(wp --version 2>&1 | head -1)"`,
		`echo "RDM-DU:$(du -sk wp-content/uploads 2>/dev/null | cut -f1)"`,
	}, "\n")
}

var (
	reMediaRoot  = regexp.MustCompile(`(?m)^RDM-ROOT:(.*)$`)
	reMediaDU    = regexp.MustCompile(`(?m)^RDM-DU:(\d+)`)
	reMediaUser  = regexp.MustCompile(`(?m)^RDM-USER:(.*)$`)
	reMediaHome  = regexp.MustCompile(`(?m)^RDM-HOME:(.*)$`)
	reMediaWPCLI = regexp.MustCompile(`(?m)^RDM-WPCLI:(.*)$`)
	reMediaErr   = regexp.MustCompile(`(?m)^RDM-ERR:(.*)$`)
)

func eersteGroep(re *regexp.Regexp, out string) string {
	if m := re.FindStringSubmatch(out); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// duBytesUit reads the du line; du reports blocks of 1 KiB.
func duBytesUit(out string) int64 {
	kb, err := strconv.ParseInt(eersteGroep(reMediaDU, out), 10, 64)
	if err != nil {
		return 0
	}
	return kb * 1024
}

// MediaProbe is the outcome of a connection check against one environment.
type MediaProbe struct {
	User      string `json:"user"`
	Home      string `json:"home"`
	Webroot   string `json:"webroot"`
	WPCLI     string `json:"wpCli"`
	UploadsKB int64  `json:"uploadsKb"`
}

func mediaProbeUit(out string) MediaProbe {
	return MediaProbe{
		User:      eersteGroep(reMediaUser, out),
		Home:      eersteGroep(reMediaHome, out),
		Webroot:   eersteGroep(reMediaRoot, out),
		WPCLI:     eersteGroep(reMediaWPCLI, out),
		UploadsKB: duBytesUit(out) / 1024,
	}
}
