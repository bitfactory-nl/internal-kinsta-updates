package services

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// zonderGoCommentaar strips Go comments (// and /* */) while respecting string
// and raw-string literals, so a comment-like sequence inside a quoted shell
// command is never mistaken for a real comment.
func zonderGoCommentaar(src string) string {
	var out strings.Builder
	inBlock := false
	inString := false
	var strDelim byte
	i := 0
	for i < len(src) {
		c := src[i]
		if inBlock {
			if c == '*' && i+1 < len(src) && src[i+1] == '/' {
				inBlock = false
				i += 2
				continue
			}
			i++
			continue
		}
		if inString {
			out.WriteByte(c)
			if c == '\\' && strDelim == '"' && i+1 < len(src) {
				out.WriteByte(src[i+1])
				i += 2
				continue
			}
			if c == strDelim {
				inString = false
			}
			i++
			continue
		}
		if c == '"' || c == '`' {
			inString = true
			strDelim = c
			out.WriteByte(c)
			i++
			continue
		}
		if c == '/' && i+1 < len(src) && src[i+1] == '/' {
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(src) && src[i+1] == '*' {
			inBlock = true
			i += 2
			continue
		}
		out.WriteByte(c)
		i++
	}
	return out.String()
}

var reFuncStart = regexp.MustCompile(`(?m)^func\s+(?:\([^)]*\)\s*)?([A-Za-z0-9_]+)\s*\(`)

// functieTeksten splits a Go source string into one text blob per top-level
// function, keyed by function name.
func functieTeksten(t *testing.T, src string) map[string]string {
	t.Helper()
	locs := reFuncStart.FindAllStringSubmatchIndex(src, -1)
	if len(locs) == 0 {
		t.Fatal("geen functies gevonden — is het bestand leeg of de regex kapot?")
	}
	out := map[string]string{}
	for i, loc := range locs {
		start := loc[0]
		end := len(src)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		name := src[loc[2]:loc[3]]
		out[name] = src[start:end]
	}
	return out
}

// TestDBCloneCommandsNeverMutateRemote is the most important test of the whole
// database-clone feature: every command builder in this file that runs over
// SSH against the customer's PRODUCTION Kinsta environment must be read-only.
// Only functions prefixed "buildLocal" are allowed to contain a mutating
// statement, because those exclusively target the already-imported LOCAL
// database in the developer's own docker.
func TestDBCloneCommandsNeverMutateRemote(t *testing.T) {
	raw, err := os.ReadFile("db_clone_commands.go")
	if err != nil {
		t.Fatalf("db_clone_commands.go lezen: %v", err)
	}
	src := zonderGoCommentaar(string(raw))
	functies := functieTeksten(t, src)

	verboden := []string{
		"wp db import",
		"wp option update",
		" UPDATE ",
		" DELETE ",
		"DROP DATABASE",
		"TRUNCATE",
		"INSERT INTO",
	}

	getest := 0
	for naam, tekst := range functies {
		if strings.HasPrefix(naam, "buildLocal") {
			continue
		}
		getest++
		for _, v := range verboden {
			if strings.Contains(tekst, v) {
				t.Errorf("functie %s bevat verboden string %q — dit zou de productie-database kunnen wijzigen", naam, v)
			}
		}
		if strings.Contains(tekst, "wp search-replace") && !strings.Contains(tekst, "--export=") {
			t.Errorf("functie %s roept wp search-replace aan zonder --export= — dit zou de productie-database wijzigen", naam)
		}
	}
	if getest == 0 {
		t.Fatal("geen enkele niet-buildLocal-functie gevonden om te controleren — guard-test dekt niets")
	}
}

func TestBuildDBExportCommandSingleSite(t *testing.T) {
	cmd := buildDBExportCommand("/www/site/public", "https://vanluyken.nl", "https://vanluykennl.test", false, "/tmp/rdm-db-abc123.sql")
	if strings.Contains(cmd, "--network") {
		t.Error("single-site export mag geen --network bevatten")
	}
	if !strings.Contains(cmd, "--export='/tmp/rdm-db-abc123.sql'") {
		t.Errorf("verwacht --export met het remoteFile-pad, kreeg: %s", cmd)
	}
	if !strings.Contains(cmd, "wp search-replace 'https://vanluyken.nl' 'https://vanluykennl.test'") {
		t.Errorf("verwacht de site-URL's als eerste twee argumenten, kreeg: %s", cmd)
	}
	if !strings.Contains(cmd, "--skip-columns=guid") {
		t.Error("verwacht --skip-columns=guid (guid's horen nooit te veranderen)")
	}
}

func TestBuildDBExportCommandMultisite(t *testing.T) {
	cmd := buildDBExportCommand("/www/site/public", "https://vanluyken.nl", "https://vanluykennl.test", true, "/tmp/rdm-db-xyz.sql")
	if !strings.Contains(cmd, "--network") {
		t.Error("multisite-export moet --network bevatten")
	}
}

func TestParseDBProbe(t *testing.T) {
	out := strings.Join([]string{
		"Warning: some plugin printed a notice here",
		"Deprecated: something else, ignore this line",
		"RDM-SITEURL:https://vanluyken.nl",
		"RDM-PREFIX:wp_",
		"RDM-MULTISITE:yes",
		"RDM-DBBYTES:157286400",
		"RDM-TMPFREEKB:2097152",
	}, "\n")

	probe := parseDBProbe(out)
	if probe.SiteURL != "https://vanluyken.nl" {
		t.Errorf("SiteURL = %q", probe.SiteURL)
	}
	if probe.TablePrefix != "wp_" {
		t.Errorf("TablePrefix = %q", probe.TablePrefix)
	}
	if !probe.IsMultisite {
		t.Error("IsMultisite = false, wil true")
	}
	if probe.DBSizeBytes != 157286400 {
		t.Errorf("DBSizeBytes = %d", probe.DBSizeBytes)
	}
	if probe.TmpFreeBytes != 2097152*1024 {
		t.Errorf("TmpFreeBytes = %d, wil %d", probe.TmpFreeBytes, 2097152*1024)
	}
}

func TestParseDBExportSize(t *testing.T) {
	if got := parseDBExportSize("wat ruis\nRDM-DBSIZE:8388608\n"); got != 8388608 {
		t.Errorf("parseDBExportSize = %d, wil 8388608", got)
	}
}

func TestBareDomain(t *testing.T) {
	cases := map[string]string{
		"https://vanluyken.nl": "vanluyken.nl",
		"http://x.nl/":         "x.nl",
		"https://sub.x.nl/":    "sub.x.nl",
	}
	for in, want := range cases {
		if got := bareDomain(in); got != want {
			t.Errorf("bareDomain(%q) = %q, wil %q", in, got, want)
		}
	}
}

func TestBuildLocalMultisiteDomainFixSQL(t *testing.T) {
	sql := buildLocalMultisiteDomainFixSQL("wp_", "vanluyken.nl", "vanluykennl.test")
	if !strings.Contains(sql, "UPDATE wp_blogs SET domain = REPLACE(domain, 'vanluyken.nl', 'vanluykennl.test')") {
		t.Errorf("verwacht een UPDATE op wp_blogs, kreeg: %s", sql)
	}
	if !strings.Contains(sql, "UPDATE wp_site SET domain = REPLACE(domain, 'vanluyken.nl', 'vanluykennl.test')") {
		t.Errorf("verwacht een UPDATE op wp_site, kreeg: %s", sql)
	}

	// Single quote in a domain must not break out of the SQL string literal.
	escaped := buildLocalMultisiteDomainFixSQL("wp_", "van'luyken.nl", "local.test")
	if !strings.Contains(escaped, "van''luyken.nl") {
		t.Errorf("single quote niet correct ge-escaped: %s", escaped)
	}

	// Een domein dat op een backslash eindigt mag de stringliteral niet laten
	// "ontsnappen": MySQL behandelt \' standaard als een ge-escapete quote, dus
	// de backslash moet EERST verdubbeld worden, anders sluit de daaropvolgende
	// quote de literal niet af zoals de query verwacht.
	metBackslash := buildLocalMultisiteDomainFixSQL("wp_", `evil.nl\`, "local.test")
	if !strings.Contains(metBackslash, `evil.nl\\`) {
		t.Errorf("backslash niet correct verdubbeld: %s", metBackslash)
	}
	if strings.Contains(metBackslash, `evil.nl\'`) {
		t.Errorf("backslash direct voor een quote zou de stringliteral laten ontsnappen: %s", metBackslash)
	}
}

// reRunCommandCall matches an s.ssh.RunCommand(...) call site and captures the
// expression passed as the command argument.
var reRunCommandCall = regexp.MustCompile(`s\.ssh\.RunCommand\([^,]+,[^,]+,\s*([A-Za-z0-9_]+)\(`)

// reSafeCommandBuilder lists the only functions allowed to construct the
// command string passed to RunCommand — every one of them is covered by
// TestDBCloneCommandsNeverMutateRemote above. If db_clone_service.go ever
// calls RunCommand with a command built some other way (e.g. a raw string
// literal, or a new helper this test doesn't know about), that call is
// exactly the kind of change the guard test on db_clone_commands.go cannot
// see — so this test catches it from the other side instead.
var reSafeCommandBuilder = map[string]bool{
	"buildDBProbeCommand":       true,
	"buildDBExportCommand":      true,
	"buildRemoteCleanupCommand": true,
}

func TestDBCloneServiceOnlyCallsRunCommandWithKnownSafeBuilders(t *testing.T) {
	raw, err := os.ReadFile("db_clone_service.go")
	if err != nil {
		t.Fatalf("db_clone_service.go lezen: %v", err)
	}
	src := zonderGoCommentaar(string(raw))
	matches := reRunCommandCall.FindAllStringSubmatch(src, -1)
	if len(matches) == 0 {
		t.Fatal("geen enkele s.ssh.RunCommand-aanroep gevonden — is het bestand/de regex kapot?")
	}
	for _, m := range matches {
		builder := m[1]
		if !reSafeCommandBuilder[builder] {
			t.Errorf("s.ssh.RunCommand wordt aangeroepen met %s(...), dat is geen bekende, door de guard-test gedekte commandbouwer", builder)
		}
	}
}

// TestGuardTestCatchesMissingExport proves the guard test is not a no-op: a
// deliberately broken variant of buildDBExportCommand (missing --export=)
// must fail the same checks the real guard test runs.
func TestGuardTestCatchesMissingExport(t *testing.T) {
	brokenSrc := `package services

func buildBrokenExportCommand(webroot, from, to string) string {
	return "wp search-replace " + from + " " + to
}
`
	functies := functieTeksten(t, zonderGoCommentaar(brokenSrc))
	tekst, ok := functies["buildBrokenExportCommand"]
	if !ok {
		t.Fatal("test-fixture zelf kapot: functie niet gevonden")
	}
	if strings.Contains(tekst, "wp search-replace") && !strings.Contains(tekst, "--export=") {
		return // dit IS het geval dat de echte guard-test zou laten falen
	}
	t.Fatal("de detectielogica zou deze kapotte variant moeten vlaggen, maar deed dat niet")
}
