package services

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/rdm/sites-tool/internal/adapters/wpplugins"
)

// Dit bestand is de (tijdelijke) lokale-map-variant naast de betaalde-plugins-repo:
// een map op deze machine met verse zips, die per project in de werkmap gezet en op
// de héél huidige branch gecommit kunnen worden. De repo blijft bestaan en blijft de
// bron voor collega's en SSH-updates; dit is de kortere weg voor wie de zips net
// van de leverancier heeft gehaald.

// LocalPaidPlugin is één zip uit de lokale map. Een onleesbare zip krijgt een
// Error in plaats van dat hij stil verdwijnt — een typefout in een download wil je
// zien, niet raden.
type LocalPaidPlugin struct {
	Slug       string `json:"slug"`
	Version    string `json:"version"`
	FileName   string `json:"fileName"`
	IsDir      bool   `json:"isDir"`
	ModifiedAt int64  `json:"modifiedAt"`
	Error      string `json:"error,omitempty"`
}

// LocalPluginRow is de vergelijking van één zip met wat er in het project staat.
type LocalPluginRow struct {
	Slug           string `json:"slug"`
	FileName       string `json:"fileName"`
	FolderVersion  string `json:"folderVersion"`
	ProjectVersion string `json:"projectVersion"` // leeg = niet in dit project
	Newer          bool   `json:"newer"`
	// Error maakt een onbruikbare bron zichtbaar in het paneel zelf; een regel die
	// stil wegvalt leest als "de tool ziet mijn map niet".
	Error string `json:"error,omitempty"`
}

// LocalPluginOverview is wat de Plugins-tab nodig heeft: de rijen plus de branch
// waarop een commit terecht zou komen. Die branch tonen is geen detail — de commit
// gaat op de huidige checkout, dus de gebruiker moet zien welke dat is.
type LocalPluginOverview struct {
	Branch string           `json:"branch"`
	Rows   []LocalPluginRow `json:"rows"`
}

// LocalApplyPlugin is het resultaat voor één plugin.
type LocalApplyPlugin struct {
	Slug   string `json:"slug"`
	From   string `json:"from"`
	To     string `json:"to"`
	Status string `json:"status"` // updated | error
	Error  string `json:"error,omitempty"`
}

// LocalApplyResult is het resultaat van één plaatsing.
type LocalApplyResult struct {
	Branch  string             `json:"branch"`
	Plugins []LocalApplyPlugin `json:"plugins"`
}

// LocalDirConfigured reports whether the local folder option is set.
func (s *PluginService) LocalDirConfigured() bool {
	return strings.TrimSpace(s.cfg.PluginRepo.LocalDir) != ""
}

// ListLocalPaidPlugins scans the configured folder for plugin zips. No folder
// configured yields (nil, nil): that is the normal state, not an error.
func (s *PluginService) ListLocalPaidPlugins() ([]LocalPaidPlugin, error) {
	dir := strings.TrimSpace(s.cfg.PluginRepo.LocalDir)
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("lokale pluginmap lezen: %w", err)
	}

	var uit []LocalPaidPlugin
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue // .DS_Store, .git en soortgenoten
		}
		rij := LocalPaidPlugin{FileName: e.Name(), IsDir: e.IsDir()}
		if info, ierr := e.Info(); ierr == nil {
			rij.ModifiedAt = info.ModTime().Unix()
		}
		switch {
		case e.IsDir():
			// Een uitgepakte pluginmap: de mapnaam is de slug, de versie komt uit
			// de header van het hoofdbestand — net als bij een zip.
			versie, perr := leesPluginDirInfo(filepath.Join(dir, e.Name()))
			if perr != nil {
				rij.Error = perr.Error()
			} else {
				rij.Slug, rij.Version = e.Name(), versie
			}
		case strings.EqualFold(filepath.Ext(e.Name()), ".zip"):
			data, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
			if rerr != nil {
				rij.Error = rerr.Error()
				break
			}
			slug, versie, perr := leesPluginZipInfo(data)
			if perr != nil {
				rij.Error = perr.Error()
				break
			}
			rij.Slug, rij.Version = slug, versie
		default:
			continue // losse bestanden die geen zip zijn horen hier niet bij
		}
		uit = append(uit, rij)
	}
	sort.Slice(uit, func(i, j int) bool { return uit[i].FileName < uit[j].FileName })
	return uit, nil
}

// LocalPluginDiff compares every readable zip in the folder against the plugin
// versions in the project's working tree.
func (s *PluginService) LocalPluginDiff(projectID string) (LocalPluginOverview, error) {
	p, ok := s.projects.Get(projectID)
	if !ok {
		return LocalPluginOverview{}, fmt.Errorf("project %q niet gevonden", projectID)
	}
	lijst, err := s.ListLocalPaidPlugins()
	if err != nil {
		return LocalPluginOverview{}, err
	}

	overzicht := LocalPluginOverview{}
	if st, serr := s.git.GetStatus(projectID); serr == nil {
		overzicht.Branch = st.Branch
	}

	pluginsDir := wpplugins.PluginsDir(p.Path)
	for _, z := range lijst {
		if z.Error != "" {
			overzicht.Rows = append(overzicht.Rows, LocalPluginRow{FileName: z.FileName, Error: z.Error})
			continue
		}
		rij := LocalPluginRow{
			Slug:           z.Slug,
			FileName:       z.FileName,
			FolderVersion:  z.Version,
			ProjectVersion: currentVersion(pluginsDir, z.Slug),
		}
		rij.Newer = rij.ProjectVersion != "" && compareVersions(rij.FolderVersion, rij.ProjectVersion) > 0
		overzicht.Rows = append(overzicht.Rows, rij)
	}
	return overzicht, nil
}

// ApplyLocalPlugins extracts the chosen zips into the project's plugin folder and
// commits each plugin on the branch the project is on right now — no new branch,
// per the workflow: the checkout is where the update belongs. Pushing stays a
// deliberate, separate action.
func (s *PluginService) ApplyLocalPlugins(projectID string, slugs []string) (LocalApplyResult, error) {
	p, ok := s.projects.Get(projectID)
	if !ok {
		return LocalApplyResult{}, fmt.Errorf("project %q niet gevonden", projectID)
	}
	if len(slugs) == 0 {
		return LocalApplyResult{}, fmt.Errorf("geen plugins geselecteerd")
	}

	st, err := s.git.GetStatus(projectID)
	if err != nil {
		return LocalApplyResult{}, err
	}
	// Commit gebeurt met wat er in de index staat; alles wat al klaargezet was zou
	// dus ongevraagd in de plugin-commit meeliften.
	if len(st.Staged) > 0 {
		return LocalApplyResult{}, fmt.Errorf(
			"er staan al %d bestand(en) klaargezet om te committen; commit of unstage die eerst", len(st.Staged))
	}

	lijst, err := s.ListLocalPaidPlugins()
	if err != nil {
		return LocalApplyResult{}, err
	}
	perSlug := map[string]LocalPaidPlugin{}
	for _, z := range lijst {
		if z.Error == "" {
			perSlug[z.Slug] = z
		}
	}

	res := LocalApplyResult{Branch: st.Branch}
	dir := strings.TrimSpace(s.cfg.PluginRepo.LocalDir)
	pluginsDir := wpplugins.PluginsDir(p.Path)

	for _, slug := range slugs {
		pr := LocalApplyPlugin{Slug: slug}
		z, ok := perSlug[slug]
		if !ok {
			pr.Status = "error"
			pr.Error = "geen leesbare zip voor deze plugin in de lokale map"
			res.Plugins = append(res.Plugins, pr)
			continue
		}
		pr.From = currentVersion(pluginsDir, slug)
		pr.To = z.Version

		var installErr error
		if z.IsDir {
			installErr = kopieerMapReplace(filepath.Join(dir, z.FileName), pluginsDir, slug)
		} else {
			data, rerr := os.ReadFile(filepath.Join(dir, z.FileName))
			if rerr != nil {
				installErr = rerr
			} else {
				installErr = extractZipReplace(data, pluginsDir, slug)
			}
		}
		if installErr != nil {
			pr.Status = "error"
			pr.Error = installErr.Error()
			res.Plugins = append(res.Plugins, pr)
			continue
		}
		relPad := "public/wp-content/plugins/" + slug
		if err := s.git.StageFiles(projectID, []string{relPad}); err != nil {
			pr.Status = "error"
			pr.Error = err.Error()
			res.Plugins = append(res.Plugins, pr)
			continue
		}
		van := pr.From
		if van == "" {
			van = "nieuw"
		}
		msg := fmt.Sprintf("chore(plugins): update %s %s→%s (betaald, lokale map)", slug, van, z.Version)
		if err := s.git.Commit(projectID, msg, false); err != nil {
			pr.Status = "error"
			pr.Error = err.Error()
			res.Plugins = append(res.Plugins, pr)
			continue
		}
		pr.Status = "updated"
		res.Plugins = append(res.Plugins, pr)
	}
	return res, nil
}

// rePluginVersion pakt de Version-regel uit een WordPress-pluginheader.
var rePluginVersion = regexp.MustCompile(`(?mi)^\s*\*?\s*Version:\s*([^\s*]+)`)

// rePluginName herkent het hoofdbestand: dat draagt de Plugin Name-header.
var rePluginName = regexp.MustCompile(`(?mi)^\s*\*?\s*Plugin Name:\s*\S`)

// leesPluginZipInfo determines slug and version from a plugin zip: the single
// top-level directory is the slug, and the version comes from the header of the
// PHP file that carries "Plugin Name:". Both wp.org and paid vendors ship this
// shape; anything else is not installable as a plugin and is reported as such.
func leesPluginZipInfo(zipData []byte) (slug, version string, err error) {
	zr, zerr := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if zerr != nil {
		return "", "", fmt.Errorf("geen leesbare zip: %w", zerr)
	}

	topmappen := map[string]bool{}
	for _, f := range zr.File {
		naam := strings.TrimPrefix(f.Name, "./")
		if i := strings.IndexByte(naam, '/'); i > 0 {
			topmappen[naam[:i]] = true
		}
	}
	if len(topmappen) != 1 {
		return "", "", fmt.Errorf("zip heeft geen enkele topmap (gevonden: %d); dit is geen pluginzip", len(topmappen))
	}
	for m := range topmappen {
		slug = m
	}

	// Alleen php-bestanden dírect in de topmap: daar hoort de header te staan.
	for _, f := range zr.File {
		naam := strings.TrimPrefix(f.Name, "./")
		if path.Dir(naam) != slug || !strings.EqualFold(path.Ext(naam), ".php") {
			continue
		}
		rc, oerr := f.Open()
		if oerr != nil {
			continue
		}
		kop, _ := io.ReadAll(io.LimitReader(rc, 8192))
		rc.Close()
		if !rePluginName.Match(kop) {
			continue
		}
		if m := rePluginVersion.FindSubmatch(kop); m != nil {
			return slug, strings.TrimSpace(string(m[1])), nil
		}
	}
	return "", "", fmt.Errorf("geen pluginheader (Plugin Name + Version) gevonden in %s/", slug)
}

// leesPluginDirInfo reads the version from an unpacked plugin folder: the PHP file
// directly in the folder that carries "Plugin Name:" also carries the version. The
// folder name is the slug, same convention as a zip's top-level directory.
func leesPluginDirInfo(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("map lezen: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".php") {
			continue
		}
		f, oerr := os.Open(filepath.Join(dir, e.Name()))
		if oerr != nil {
			continue
		}
		kop, _ := io.ReadAll(io.LimitReader(f, 8192))
		f.Close()
		if !rePluginName.Match(kop) {
			continue
		}
		if m := rePluginVersion.FindSubmatch(kop); m != nil {
			return strings.TrimSpace(string(m[1])), nil
		}
	}
	return "", fmt.Errorf("geen pluginheader (Plugin Name + Version) gevonden in %s/", filepath.Base(dir))
}

// kopieerMapReplace copies an unpacked plugin folder into pluginsDir/<slug> with
// the same discipline as extractZipReplace: stage the full copy first, only then
// swap the old folder out. macOS droppings (.DS_Store), VCS folders and symlinks
// stay behind — those belong to the source machine, not in a customer repo.
func kopieerMapReplace(srcDir, pluginsDir, slug string) error {
	src, err := os.Stat(srcDir)
	if err != nil || !src.IsDir() {
		return fmt.Errorf("bronmap ontbreekt: %s", srcDir)
	}

	temp, err := os.MkdirTemp(pluginsDir, "."+slug+".tmp-copy-")
	if err != nil {
		return fmt.Errorf("tijdelijke map aanmaken: %w", err)
	}
	defer os.RemoveAll(temp)
	staged := filepath.Join(temp, slug)

	err = filepath.WalkDir(srcDir, func(pad string, d os.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		naam := d.Name()
		if naam == ".git" || naam == ".svn" || naam == "node_modules" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if naam == ".DS_Store" {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil // symlinks kunnen buiten de bron wijzen; overslaan is veilig
		}
		rel, rerr := filepath.Rel(srcDir, pad)
		if rerr != nil {
			return rerr
		}
		dest := filepath.Join(staged, rel)
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		return kopieerBestand(pad, dest)
	})
	if err != nil {
		return fmt.Errorf("kopiëren: %w", err)
	}

	// Zelfde slot als extractZipReplace: pas als de kopie compleet klaarstaat gaat
	// de oude map weg.
	target := filepath.Join(pluginsDir, slug)
	if info, err := os.Stat(staged); err != nil || !info.IsDir() {
		return fmt.Errorf("kopie is niet compleet klaargezet")
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("oude plugin verwijderen: %w", err)
	}
	if err := os.Rename(staged, target); err != nil {
		return fmt.Errorf("kopie op zijn plek zetten: %w", err)
	}
	return nil
}

func kopieerBestand(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
