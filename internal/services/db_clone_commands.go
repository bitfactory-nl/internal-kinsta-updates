package services

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/rdm/sites-tool/internal/domain"
)

// buildDBProbeCommand collects, in one round trip, everything a clone needs
// to know before it starts: the canonical site URL, the table prefix, whether
// the site is multisite, the database size, and free space in /tmp for the
// temporary dump.
func buildDBProbeCommand(webroot string) string {
	return strings.Join([]string{
		zoekWebroot(webroot),
		`if [ -z "$root" ] || [ ! -f "$root/wp-config.php" ]; then echo "RDM-ERR:geen wp-config.php gevonden"; exit 3; fi`,
		`cd "$root" || exit 3`,
		`echo "RDM-SITEURL:$(wp option get siteurl 2>&1)"`,
		`echo "RDM-PREFIX:$(wp config get table_prefix 2>&1)"`,
		`echo "RDM-MULTISITE:$(wp eval 'echo is_multisite() ? "yes" : "no";' 2>&1)"`,
		`echo "RDM-DBBYTES:$(wp eval 'global $wpdb; $r=$wpdb->get_row("SELECT SUM(data_length+index_length) AS b FROM information_schema.tables WHERE table_schema=DATABASE()"); echo intval($r->b);' 2>&1)"`,
		`echo "RDM-TMPFREEKB:$(df -Pk /tmp 2>/dev/null | tail -1 | awk '{print $4}')"`,
	}, "\n")
}

var (
	reDBSiteURL    = regexp.MustCompile(`(?m)^RDM-SITEURL:(.*)$`)
	reDBPrefix     = regexp.MustCompile(`(?m)^RDM-PREFIX:(.*)$`)
	reDBMultisite  = regexp.MustCompile(`(?m)^RDM-MULTISITE:(.*)$`)
	reDBBytes      = regexp.MustCompile(`(?m)^RDM-DBBYTES:(\d+)`)
	reDBTmpFreeKB  = regexp.MustCompile(`(?m)^RDM-TMPFREEKB:(\d+)`)
	reDBExportSize = regexp.MustCompile(`(?m)^RDM-DBSIZE:(\d+)`)
)

// parseDBProbe turns buildDBProbeCommand's raw stdout into a DBProbe.
func parseDBProbe(out string) domain.DBProbe {
	dbBytes, _ := strconv.ParseInt(eersteGroep(reDBBytes, out), 10, 64)
	tmpFreeKB, _ := strconv.ParseInt(eersteGroep(reDBTmpFreeKB, out), 10, 64)
	return domain.DBProbe{
		SiteURL:      eersteGroep(reDBSiteURL, out),
		TablePrefix:  eersteGroep(reDBPrefix, out),
		IsMultisite:  eersteGroep(reDBMultisite, out) == "yes",
		DBSizeBytes:  dbBytes,
		TmpFreeBytes: tmpFreeKB * 1024,
	}
}

// buildDBExportCommand builds the ONLY remote command that touches the
// production database in the whole clone pipeline, and it never mutates it:
// `wp search-replace` is always called with --export, which writes the
// transformed result to a file instead of the database (confirmed via the
// wp-cli documentation). A guard test (TestDBCloneCommandsNeverMutateRemote)
// enforces this invariant on every non-"buildLocal" function in this file.
func buildDBExportCommand(webroot, prodSiteURL, localURL string, multisite bool, remoteFile string) string {
	network := ""
	if multisite {
		network = " --network"
	}
	lines := []string{
		"cd " + shellQuote(webroot) + " && rm -f " + shellQuote(remoteFile) + " " + shellQuote(remoteFile+".gz") + " \\",
		"  && nice -n 19 wp search-replace " + shellQuote(prodSiteURL) + " " + shellQuote(localURL) +
			" --all-tables-with-prefix --skip-columns=guid" + network + " --export=" + shellQuote(remoteFile) + " 2>&1 \\",
		"  && gzip -f " + shellQuote(remoteFile) + " \\",
		"  && echo \"RDM-DBSIZE:$(wc -c < " + shellQuote(remoteFile+".gz") + ")\"",
	}
	return strings.Join(lines, "\n")
}

// parseDBExportSize reads the RDM-DBSIZE line buildDBExportCommand prints
// after gzipping the dump.
func parseDBExportSize(out string) int64 {
	n, _ := strconv.ParseInt(eersteGroep(reDBExportSize, out), 10, 64)
	return n
}

// buildRemoteCleanupCommand removes the temporary dump (and its gzip) left on
// the server by buildDBExportCommand. Best-effort: the caller ignores its
// error.
func buildRemoteCleanupCommand(remoteFile string) string {
	return "rm -f " + shellQuote(remoteFile) + " " + shellQuote(remoteFile+".gz")
}

// bareDomain strips a scheme and trailing slash, turning
// "https://vanluyken.nl/" into "vanluyken.nl".
func bareDomain(url string) string {
	d := strings.TrimPrefix(strings.TrimPrefix(url, "https://"), "http://")
	return strings.TrimSuffix(d, "/")
}

// escapeSQLString escapes single quotes for a SQL string literal.
func escapeSQLString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// buildLocalMultisiteDomainFixSQL touches ONLY the two WordPress core columns
// that store a bare hostname as a guaranteed plain string, never PHP-serialized:
// <prefix>blogs.domain and <prefix>site.domain (both live under the network's
// base prefix, not a per-site prefix, even on multisite). This is deliberately
// NOT a blanket search-replace across wp_options — that could break serialized
// widget/plugin data containing the URL as a substring. The blanket,
// serialization-safe replacement for everything else already happens in
// buildDBExportCommand's `wp search-replace --export` step; this SQL only
// closes the one gap that step cannot reach, because wp_blogs/wp_site store
// the domain without a scheme (a full-URL search-replace pass never matches
// it). This function is prefixed "buildLocal" because it is only ever run
// against the already-imported LOCAL database, never against production.
func buildLocalMultisiteDomainFixSQL(tablePrefix, prodBareDomain, localBareDomain string) string {
	prod := escapeSQLString(prodBareDomain)
	local := escapeSQLString(localBareDomain)
	return strings.Join([]string{
		"UPDATE " + tablePrefix + "blogs SET domain = REPLACE(domain, '" + prod + "', '" + local + "');",
		"UPDATE " + tablePrefix + "site SET domain = REPLACE(domain, '" + prod + "', '" + local + "');",
	}, "\n")
}
