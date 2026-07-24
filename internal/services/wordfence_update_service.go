// internal/services/wordfence_update_service.go
package services

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/wporg"
	"github.com/rdm/sites-tool/internal/adapters/wpplugins"
	"github.com/rdm/sites-tool/internal/domain"
)

// pluginDownloader is the subset of the wp.org client used here (test seam).
type pluginDownloader interface {
	LatestVersion(ctx context.Context, slug string) (string, string, error)
	Download(ctx context.Context, url string) ([]byte, error)
}

type UpdateSelection struct {
	ProjectID string   `json:"projectId"`
	Slugs     []string `json:"slugs"`
}

type PluginUpdateResult struct {
	Slug   string `json:"slug"`
	From   string `json:"from"`
	To     string `json:"to"`
	Status string `json:"status"` // updated | manual | error
	Error  string `json:"error"`
}

type ProjectUpdateResult struct {
	ProjectID   string               `json:"projectId"`
	ProjectName string               `json:"projectName"`
	Status      string               `json:"status"` // updated | needs_stash | skipped_no_release | error | nothing
	Branch      string               `json:"branch"`
	Error       string               `json:"error"`
	Plugins     []PluginUpdateResult `json:"plugins"`
}

type WordfenceUpdateService struct {
	git      *GitService
	projects *ProjectService
	wporg    pluginDownloader
}

func NewWordfenceUpdateService(git *GitService, projects *ProjectService) *WordfenceUpdateService {
	return &WordfenceUpdateService{git: git, projects: projects, wporg: wporg.NewClient()}
}

func (s *WordfenceUpdateService) projectByID(id string) (domain.Project, bool) {
	for _, p := range s.projects.List() {
		if p.ID == id {
			return p, true
		}
	}
	return domain.Project{}, false
}

// ApplyProject updates the selected plugins in one project. When the worktree
// is dirty and autoStash is false, it returns status "needs_stash" without
// changing anything; the frontend re-calls with autoStash=true after the user
// confirms.
func (s *WordfenceUpdateService) ApplyProject(sel UpdateSelection, autoStash bool) ProjectUpdateResult {
	p, ok := s.projectByID(sel.ProjectID)
	res := ProjectUpdateResult{ProjectID: sel.ProjectID, ProjectName: p.DisplayName}
	if !ok {
		res.Status = "error"
		res.Error = "project niet gevonden"
		return res
	}
	if len(sel.Slugs) == 0 {
		res.Status = "nothing"
		return res
	}

	// 1. Default branch must match release/*.
	def, err := s.git.DefaultBranch(sel.ProjectID)
	if err != nil {
		res.Status = "error"
		res.Error = err.Error()
		return res
	}
	if !strings.HasPrefix(def, "release/") {
		res.Status = "skipped_no_release"
		res.Error = fmt.Sprintf("default branch %q voldoet niet aan release/*", def)
		return res
	}

	// 2. Dirty check.
	st, err := s.git.GetStatus(sel.ProjectID)
	if err != nil {
		res.Status = "error"
		res.Error = err.Error()
		return res
	}
	if isDirty(st) {
		if !autoStash {
			res.Status = "needs_stash"
			return res
		}
		if err := s.git.StashSave(sel.ProjectID, "wordfence-update auto-stash"); err != nil {
			res.Status = "error"
			res.Error = "stash mislukt: " + err.Error()
			return res
		}
	}

	// 3. Create branch from default.
	branch := "security/wordfence-" + time.Now().Format("2006-01-02")
	if err := s.git.CreateBranch(sel.ProjectID, branch, def); err != nil {
		res.Status = "error"
		res.Error = "branch aanmaken mislukt: " + err.Error()
		return res
	}
	res.Branch = branch

	// 4. Update each plugin.
	pluginsDir := wpplugins.PluginsDir(p.Path)
	ctx := context.Background()
	anyUpdated := false
	for _, slug := range sel.Slugs {
		pr := PluginUpdateResult{Slug: slug}
		installed := currentVersion(pluginsDir, slug)
		pr.From = installed

		ver, url, err := s.wporg.LatestVersion(ctx, slug)
		if err != nil {
			pr.Status = "manual"
			pr.Error = "niet op wp.org"
			res.Plugins = append(res.Plugins, pr)
			continue
		}
		pr.To = ver
		data, err := s.wporg.Download(ctx, url)
		if err != nil {
			pr.Status = "error"
			pr.Error = err.Error()
			res.Plugins = append(res.Plugins, pr)
			continue
		}
		if err := extractZipReplace(data, pluginsDir, slug); err != nil {
			pr.Status = "error"
			pr.Error = err.Error()
			res.Plugins = append(res.Plugins, pr)
			continue
		}
		if err := s.git.StageAll(sel.ProjectID); err != nil {
			pr.Status = "error"
			pr.Error = err.Error()
			res.Plugins = append(res.Plugins, pr)
			continue
		}
		msg := fmt.Sprintf("fix(security): update %s %s→%s (Wordfence)", slug, installed, ver)
		if err := s.git.Commit(sel.ProjectID, msg, false); err != nil {
			pr.Status = "error"
			pr.Error = err.Error()
			res.Plugins = append(res.Plugins, pr)
			continue
		}
		pr.Status = "updated"
		anyUpdated = true
		res.Plugins = append(res.Plugins, pr)
	}

	if anyUpdated {
		res.Status = "updated"
	} else {
		res.Status = "nothing"
	}
	return res
}

func isDirty(st domain.GitStatus) bool {
	return len(st.Staged) > 0 || len(st.Unstaged) > 0 || len(st.Untracked) > 0 || len(st.Conflicted) > 0
}

func currentVersion(pluginsDir, slug string) string {
	// Reuse the reader against the parent-of-pluginsDir project root.
	// pluginsDir = <root>/public/wp-content/plugins
	root := filepath.Dir(filepath.Dir(filepath.Dir(pluginsDir)))
	installed, _ := wpplugins.ReadInstalled(root)
	for _, ip := range installed {
		if ip.Slug == slug {
			return ip.Version
		}
	}
	return ""
}

// extractZipReplace removes plugins/<slug> and extracts the plugin zip into it.
// wp.org zips contain a top-level <slug>/ directory.
func extractZipReplace(zipData []byte, pluginsDir, slug string) error {
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	target := filepath.Join(pluginsDir, slug)
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("remove old plugin: %w", err)
	}
	cleanBase := filepath.Clean(pluginsDir) + string(os.PathSeparator)
	for _, f := range zr.File {
		dest := filepath.Join(pluginsDir, f.Name)
		if !strings.HasPrefix(filepath.Clean(dest)+string(os.PathSeparator), cleanBase) &&
			filepath.Clean(dest) != filepath.Clean(pluginsDir) {
			return fmt.Errorf("unsafe path in zip: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			rc.Close()
			return err
		}
		out.Close()
		rc.Close()
	}
	return nil
}
