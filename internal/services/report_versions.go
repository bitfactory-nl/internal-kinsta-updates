package services

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/endoflife"
	"github.com/rdm/sites-tool/internal/adapters/gitcli"
	"github.com/rdm/sites-tool/internal/domain"
)

// reVersionToken pakt het eerste numerieke versiedeel uit een string.
var reVersionToken = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)*`)

// normalizeVersion strips vendor-ruis van een versietag én van handmatige
// invoer: "php8.3" -> "8.3", "24.16.0-bf3" -> "24.16.0", "PHP 8.3.32" ->
// "8.3.32", "8.3 (Kinsta prod)" -> "8.3". Tags met een niet geresolvede
// variabele ("${TAG_NODE}") of zonder cijfers ("latest") leveren "" op.
func normalizeVersion(raw string) string {
	v := strings.TrimSpace(strings.ToLower(raw))
	if v == "" || strings.Contains(v, "$") {
		return ""
	}
	return reVersionToken.FindString(v)
}

var (
	rePHPFrom  = regexp.MustCompile(`(?m)^\s*FROM\s+\S*php:([^\s]+)`)
	reNodeImg  = regexp.MustCompile(`(?m)^\s*image:\s*\S*node:([^\s"']+)`)
	reNodeFrom = regexp.MustCompile(`(?m)^\s*FROM\s+\S*node:([^\s]+)`)
)

// phpFromDockerfile leest de PHP-versie uit de eerste php-FROM-regel
// (bitfactory service-php of plain php-image).
func phpFromDockerfile(content []byte) string {
	m := rePHPFrom.FindSubmatch(content)
	if m == nil {
		return ""
	}
	return normalizeVersion(string(m[1]))
}

// nodeFromCompose leest de node-image-tag uit docker-compose.yaml.
func nodeFromCompose(content []byte) string {
	m := reNodeImg.FindSubmatch(content)
	if m == nil {
		return ""
	}
	return normalizeVersion(string(m[1]))
}

// nodeFromDockerfile leest de node-versie uit een FROM-regel (frontend stage).
func nodeFromDockerfile(content []byte) string {
	m := reNodeFrom.FindSubmatch(content)
	if m == nil {
		return ""
	}
	return normalizeVersion(string(m[1]))
}

// cycleActive meldt of een cycle nog niet EOL is op t.
func cycleActive(c endoflife.Cycle, t time.Time) bool {
	if c.EOL.IsDate {
		return c.EOL.Date.After(t)
	}
	return !c.EOL.Bool // eol=false: nog geen EOL-datum bekend
}

// latestActive kiest de "Laatste versie" per product:
// php: hoogste cycle met lopende active support; nodejs: hoogste cycle die al
// LTS is en nog niet EOL; mariadb: hoogste LTS-cycle die nog niet EOL is;
// overig (wordpress): hoogste niet-EOL cycle. Geeft de laatste patchrelease
// van die cycle terug.
func latestActive(product string, cycles []endoflife.Cycle, now time.Time) string {
	var best *endoflife.Cycle
	for i := range cycles {
		c := &cycles[i]
		var ok bool
		switch product {
		case "php":
			ok = c.Support.IsDate && c.Support.Date.After(now)
		case "nodejs":
			ok = c.LTS.IsDate && !c.LTS.Date.After(now) && cycleActive(*c, now)
		case "mariadb":
			ok = !c.LTS.IsDate && c.LTS.Bool && cycleActive(*c, now)
		default:
			ok = cycleActive(*c, now)
		}
		if !ok {
			continue
		}
		if best == nil || compareVersions(c.Cycle, best.Cycle) > 0 {
			best = c
		}
	}
	if best == nil {
		return ""
	}
	return best.Latest
}

// supportedUntil geeft de EOL-datum (dd-mm-jjjj) van de cycle waar current in
// valt, of "" als de cycle onbekend is of geen EOL-datum heeft.
func supportedUntil(current string, cycles []endoflife.Cycle) string {
	current = normalizeVersion(current)
	if current == "" {
		return ""
	}
	var match *endoflife.Cycle
	for i := range cycles {
		c := &cycles[i]
		if current != c.Cycle && !strings.HasPrefix(current, c.Cycle+".") {
			continue
		}
		if match == nil || len(c.Cycle) > len(match.Cycle) {
			match = c // langste (meest specifieke) cycle wint
		}
	}
	if match == nil || !match.EOL.IsDate {
		return ""
	}
	return match.EOL.Date.Format("02-01-2006")
}

// repoFileReader leest een projectbestand voor de rapportage-prefill
// (test seam voor GitRepoFiles).
type repoFileReader interface {
	ReadProjectFile(p domain.Project, relPath string) ([]byte, error)
}

// GitRepoFiles leest bestanden op origin/<default branch> zodat het rapport
// de gecommitte staat beschrijft, ook als de werkmap op een feature-branch
// staat. Zonder bruikbare ref valt hij terug op de werkmap.
type GitRepoFiles struct{}

func (GitRepoFiles) ReadProjectFile(p domain.Project, relPath string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if ref := projectFileRef(ctx, p.Path); ref != "" {
		if b, err := gitcli.ShowFile(ctx, p.Path, ref, relPath); err == nil {
			return b, nil
		}
	}
	return os.ReadFile(filepath.Join(p.Path, relPath))
}

// projectFileRef resolvet de git-ref om projectbestanden van te lezen:
// origin/<default> als die bestaat, anders de lokale default branch, anders
// "" (werkmap).
func projectFileRef(ctx context.Context, path string) string {
	def, err := gitcli.DefaultBranch(ctx, path)
	if err != nil || def == "" {
		return ""
	}
	if gitcli.RefExists(ctx, path, "origin/"+def) {
		return "origin/" + def
	}
	if gitcli.RefExists(ctx, path, def) {
		return def
	}
	return ""
}
