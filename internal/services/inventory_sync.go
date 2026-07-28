package services

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/gitcli"
	"github.com/rdm/sites-tool/internal/adapters/github"
	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
)

// syncSource is alles wat de syncer nodig heeft om te bepalen of een project
// achterloopt op GitHub, en om het bij te werken (test seam).
type syncSource interface {
	BranchSHA(ctx context.Context, repo, branch string) (string, error)
	LocalRefSHA(ctx context.Context, path, ref string) (string, error)
	DefaultBranchName(ctx context.Context, path string) (string, error)
	RemoteURL(ctx context.Context, path string) (string, error)
	FetchRepo(ctx context.Context, path string) error
}

// syncTTL bepaalt hoe lang een SHA-check geldig blijft. Kort genoeg om een
// merge snel op te pikken, lang genoeg om herhaald openen van de overzichten
// niet in API-calls te laten lopen.
const syncTTL = 5 * time.Minute

// syncWorkers bound het aantal parallelle checks/fetches.
const syncWorkers = 6

// inventorySyncer houdt de lokale origin-refs gelijk aan GitHub, zodat de
// GitHub-kolom in de overzichten de echte stand van de release-branch toont
// zonder dat de gebruiker handmatig "Fetch alles" hoeft te klikken.
//
// Volledig best-effort: zonder token, zonder netwerk of bij een API-fout
// gebeurt er simpelweg niets en vallen de overzichten terug op de laatst
// gefetchte stand.
type inventorySyncer struct {
	src syncSource
	now func() time.Time

	mu      sync.Mutex
	checked map[string]time.Time // projectpad -> moment van laatste check
}

func newInventorySyncer(src syncSource) *inventorySyncer {
	return &inventorySyncer{src: src, now: time.Now, checked: map[string]time.Time{}}
}

// Sync werkt de origin-refs van projects bij waar GitHub vooruit is.
func (s *inventorySyncer) Sync(ctx context.Context, projects []domain.Project) {
	if s.src == nil {
		return
	}
	todo := make([]domain.Project, 0, len(projects))
	for _, p := range projects {
		if p.Path != "" && s.due(p.Path) {
			todo = append(todo, p)
		}
	}
	if len(todo) == 0 {
		return
	}

	sem := make(chan struct{}, syncWorkers)
	var wg sync.WaitGroup
	for _, p := range todo {
		wg.Add(1)
		sem <- struct{}{}
		go func(p domain.Project) {
			defer wg.Done()
			defer func() { <-sem }()
			s.syncOne(ctx, p)
		}(p)
	}
	wg.Wait()
}

// due meldt of dit project opnieuw gecheckt mag worden (TTL verstreken).
func (s *inventorySyncer) due(path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.now().Sub(s.checked[path]) >= syncTTL
}

// markChecked onthoudt dat dit project net gecheckt is.
func (s *inventorySyncer) markChecked(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checked[path] = s.now()
}

// syncOne fetcht één project als GitHub vooruit is op de lokale origin-ref.
func (s *inventorySyncer) syncOne(ctx context.Context, p domain.Project) {
	def, err := s.src.DefaultBranchName(ctx, p.Path)
	if err != nil || def == "" {
		return // geen git-repo of geen default branch
	}
	remote, err := s.src.RemoteURL(ctx, p.Path)
	if err != nil || strings.TrimSpace(remote) == "" {
		return // geen remote: niets om tegen te vergelijken
	}
	owner, repo, err := github.ParseRepoFromRemote(remote)
	if err != nil {
		return // geen GitHub-remote
	}

	remoteSHA, err := s.src.BranchSHA(ctx, owner+"/"+repo, def)
	if err != nil || remoteSHA == "" {
		return // geen token, geen netwerk of onbekende branch: best-effort
	}
	// Vanaf hier is de check gelukt, dus de TTL mag gaan lopen — ook als er
	// niets te fetchen valt.
	s.markChecked(p.Path)

	localSHA, err := s.src.LocalRefSHA(ctx, p.Path, "origin/"+def)
	if err == nil && localSHA == remoteSHA {
		return // lokale stand is al gelijk
	}
	_ = s.src.FetchRepo(ctx, p.Path) // mislukt fetchen is niet fataal
}

// errNoGithubToken markeert een ontbrekend/onleesbaar token. De syncer
// behandelt dit als "geen check mogelijk" en laat de overzichten ongemoeid.
var errNoGithubToken = errors.New("github token niet geconfigureerd")

// gitSyncSource is de productie-implementatie van syncSource.
type gitSyncSource struct {
	cfg *config.Global
}

func (g gitSyncSource) client() (*github.ActionsClient, error) {
	token, err := config.ResolveSecret(g.cfg.PluginRepo.GithubToken)
	if err != nil || token == "" {
		return nil, errNoGithubToken
	}
	return github.NewActionsClient(token), nil
}

func (g gitSyncSource) BranchSHA(ctx context.Context, repo, branch string) (string, error) {
	c, err := g.client()
	if err != nil {
		return "", err
	}
	return c.BranchSHA(ctx, repo, branch)
}

func (g gitSyncSource) LocalRefSHA(ctx context.Context, path, ref string) (string, error) {
	return gitcli.Run(ctx, path, "rev-parse", ref)
}

func (g gitSyncSource) DefaultBranchName(ctx context.Context, path string) (string, error) {
	return gitcli.DefaultBranch(ctx, path)
}

func (g gitSyncSource) RemoteURL(ctx context.Context, path string) (string, error) {
	return gitcli.Run(ctx, path, "remote", "get-url", "origin")
}

func (g gitSyncSource) FetchRepo(ctx context.Context, path string) error {
	return gitcli.Fetch(ctx, path)
}
