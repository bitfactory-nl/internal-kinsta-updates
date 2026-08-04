package services

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/rdm/sites-tool/internal/domain"
)

// uploadsPad is the uploads directory relative to the WordPress webroot. Kinsta
// installs are plain WordPress layouts, so this is the standard path.
const uploadsPad = "wp-content/uploads"

// buildUploadsListCommand lists the top-level directories inside
// wp-content/uploads with their sizes, in one round trip. Read-only.
//
// Only the first level is listed: WordPress buckets media per year (2025,
// 2026), plus plugin directories (cache, smush, wpml) and, on multisite,
// `sites/<blogid>`. That is the granularity a person actually chooses at; going
// deeper would produce hundreds of lines nobody scrolls through.
func buildUploadsListCommand(webroot string) string {
	return strings.Join([]string{
		zoekWebroot(webroot),
		`if [ -z "$root" ] || [ ! -f "$root/wp-config.php" ]; then echo "RDM-ERR:geen wp-config.php gevonden"; exit 3; fi`,
		`cd "$root" || exit 3`,
		`if [ ! -d ` + shellQuote(uploadsPad) + ` ]; then echo "RDM-ERR:geen ` + uploadsPad + ` gevonden"; exit 3; fi`,
		`cd ` + shellQuote(uploadsPad) + ` || exit 3`,
		// -maxdepth 1 -mindepth 1 -type d: alleen de eerste laag mappen, niet de
		// map zelf. du -sk per map geeft de grootte in KiB.
		`for d in $(find . -maxdepth 1 -mindepth 1 -type d | sed 's|^\./||' | sort); do echo "RDM-DIR:$(du -sk "$d" 2>/dev/null | cut -f1)	$d"; done`,
		// Losse bestanden in de wortel van uploads (komt voor bij oude sites) als
		// een eigen pseudo-map, zodat ze niet stil worden overgeslagen.
		`echo "RDM-ROOTFILES:$(find . -maxdepth 1 -type f | wc -l | tr -d ' ')"`,
	}, "\n")
}

var (
	reUploadDir       = regexp.MustCompile(`(?m)^RDM-DIR:(\d+)\t(.+)$`)
	reUploadRootFiles = regexp.MustCompile(`(?m)^RDM-ROOTFILES:(\d+)`)
)

// parseUploadFolders turns buildUploadsListCommand's stdout into folder rows.
func parseUploadFolders(out string) []domain.UploadFolder {
	matches := reUploadDir.FindAllStringSubmatch(out, -1)
	folders := make([]domain.UploadFolder, 0, len(matches))
	for _, m := range matches {
		kb, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			continue
		}
		naam := strings.TrimSpace(m[2])
		if naam == "" {
			continue
		}
		folders = append(folders, domain.UploadFolder{Name: naam, Bytes: kb * 1024})
	}
	return folders
}

// parseUploadRootFileCount reads how many loose files sit directly in uploads.
func parseUploadRootFileCount(out string) int {
	n, _ := strconv.Atoi(eersteGroep(reUploadRootFiles, out))
	return n
}

// buildUploadsTarCommand streams one folder inside uploads as a gzipped tar on
// stdout. Read-only on the server, and nothing is written to its disk: the
// archive only exists in the pipe, so no temporary space is needed even for a
// folder of many gigabytes.
func buildUploadsTarCommand(webroot, folder string) string {
	return strings.Join([]string{
		zoekWebroot(webroot),
		`if [ -z "$root" ]; then exit 3; fi`,
		`cd "$root/` + uploadsPad + `" || exit 3`,
		// nice: het inpakken mag de klantcontainer niet opslokken.
		`nice -n 19 tar czf - ` + shellQuote(folder),
	}, "\n")
}

// buildUploadsRootFilesTarCommand does the same for the loose files directly in
// uploads (no subdirectories), which buildUploadsTarCommand's per-folder form
// cannot express.
func buildUploadsRootFilesTarCommand(webroot string) string {
	return strings.Join([]string{
		zoekWebroot(webroot),
		`if [ -z "$root" ]; then exit 3; fi`,
		`cd "$root/` + uploadsPad + `" || exit 3`,
		`nice -n 19 find . -maxdepth 1 -type f -print0 | nice -n 19 tar czf - --null -T -`,
	}, "\n")
}
