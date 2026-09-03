package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/gitcli"
	"github.com/rdm/sites-tool/internal/domain"
)

// orgCloneSlot is het claim-slot voor clone-runs. Bewust een ander slot dan
// orgSyncSlot: een lopende sync (die alleen leest) hoeft een clone niet te
// blokkeren en omgekeerd, maar twee clone-runs door elkaar wél — die zouden
// naar dezelfde doelmappen kunnen schrijven.
const orgCloneSlot = "org-clone"

// orgCloneTimeout begrenst een hele clone-run, orgCloneRepoTimeout één repo.
// WordPress-repo's met veel historie kunnen minuten duren, dus de per-repo
// grens is ruim; het plafond op de hele run voorkomt dat een selectie van
// tientallen repo's de service urenlang bezet houdt.
const (
	orgCloneTimeout     = 60 * time.Minute
	orgCloneRepoTimeout = 15 * time.Minute
)

// orgCloneProgressEvent is de naam van het Wails-event dat de UI meeleest.
const orgCloneProgressEvent = "orgclone:progress"

// Clone haalt de opgegeven org-repo's lokaal binnen in de eerste
// geconfigureerde projects-root, met de repo-naam als mapnaam. Dat is precies
// waar ProjectService.Scan() naar kijkt, dus de clone verschijnt daarna als
// gewoon project.
//
// De run is sequentieel en gaat door na een fout: elke repo levert een eigen
// OrgCloneOutcome op. Een bestaande doelmap wordt overgeslagen, nooit
// overschreven.
func (s *OrgSyncService) Clone(names []string) (domain.OrgCloneResult, error) {
	if len(names) == 0 {
		return domain.OrgCloneResult{}, fmt.Errorf("geen repo's geselecteerd")
	}
	if !s.claim(orgCloneSlot) {
		return domain.OrgCloneResult{}, fmt.Errorf("er loopt al een clone")
	}
	defer s.release(orgCloneSlot)

	root, err := s.cloneRoot()
	if err != nil {
		return domain.OrgCloneResult{}, err
	}

	targets, err := s.resolveCloneTargets(names)
	if err != nil {
		return domain.OrgCloneResult{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), orgCloneTimeout)
	defer cancel()

	result := domain.OrgCloneResult{Root: root, Outcomes: make([]domain.OrgCloneOutcome, 0, len(targets))}
	for i, t := range targets {
		s.emitClone(domain.OrgCloneProgress{Phase: "bezig", Repo: t.name, Done: i, Total: len(targets)})
		outcome := s.cloneOne(ctx, root, t)
		result.Outcomes = append(result.Outcomes, outcome)
		switch outcome.Status {
		case domain.OrgCloneCloned:
			result.Cloned++
		case domain.OrgCloneSkipped:
			result.Skipped++
		default:
			result.Failed++
		}
	}

	// Eén rescan aan het eind in plaats van per repo: pas hierna kent
	// ProjectService de nieuwe checkouts, en dat is wat de org-sync-matching
	// gebruikt om "Lokaal" te vullen.
	if result.Cloned > 0 {
		if _, serr := s.projects.Scan(); serr != nil {
			// De clones staan er al; een mislukte rescan is hinderlijk, niet fataal.
			result.Outcomes = append(result.Outcomes, domain.OrgCloneOutcome{
				Status:  domain.OrgCloneFailed,
				Message: fmt.Sprintf("projecten opnieuw scannen mislukt: %v", serr),
			})
			result.Failed++
		}
	}

	s.emitClone(domain.OrgCloneProgress{Phase: "klaar", Done: len(targets), Total: len(targets)})
	return result, nil
}

// cloneTarget is één gevalideerde clone-opdracht: de mapnaam en de remote-URL,
// beide afgeleid van een repo die daadwerkelijk in de org-lijst staat.
type cloneTarget struct {
	name string
	url  string
}

// cloneRoot bepaalt de doelmap: de eerste geconfigureerde projects-root. Er is
// bewust geen fallback naar de cwd — de working directory van een .app-bundle
// is onvoorspelbaar, dus zonder root is "waar dan ook clonen" erger dan een
// duidelijke fout.
func (s *OrgSyncService) cloneRoot() (string, error) {
	if s.projects == nil {
		return "", fmt.Errorf("projectservice niet beschikbaar")
	}
	roots := s.projects.GetRoots()
	for _, r := range roots {
		if strings.TrimSpace(r) == "" {
			continue
		}
		root := expandHome(strings.TrimSpace(r))
		info, err := os.Stat(root)
		if err != nil {
			return "", fmt.Errorf("projectmap %q is niet te openen: %w", root, err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("projectmap %q is geen map", root)
		}
		return root, nil
	}
	return "", fmt.Errorf("geen projectmap ingesteld (zie Instellingen)")
}

// resolveCloneTargets zet de aangevraagde namen om in clone-opdrachten. Elke
// naam moet in het laatst gesynchroniseerde org-resultaat voorkomen; die
// controle is niet alleen een nette foutmelding maar ook de veiligheidsgrens,
// want daardoor komt de mapnaam altijd uit GitHub en nooit rechtstreeks uit de
// aanroep (geen "../"-streken).
func (s *OrgSyncService) resolveCloneTargets(names []string) ([]cloneTarget, error) {
	cache, err := s.loadCache()
	if err != nil {
		return nil, fmt.Errorf("org-sync cache lezen: %w", err)
	}
	if len(cache.Result.Repos) == 0 {
		return nil, fmt.Errorf("nog nooit gesynchroniseerd: synchroniseer eerst de organisatie")
	}

	byName := make(map[string]domain.OrgSyncRepo, len(cache.Result.Repos))
	for _, r := range cache.Result.Repos {
		byName[strings.ToLower(r.Name)] = r
	}

	seen := make(map[string]bool, len(names))
	targets := make([]cloneTarget, 0, len(names))
	for _, raw := range names {
		key := strings.ToLower(strings.TrimSpace(raw))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true

		repo, ok := byName[key]
		if !ok {
			return nil, fmt.Errorf("repo %q staat niet in de laatst opgehaalde org-lijst", raw)
		}
		if !isSafeDirName(repo.Name) {
			return nil, fmt.Errorf("repo %q heeft een naam die niet als mapnaam kan dienen", repo.Name)
		}
		url, uerr := cloneURL(repo.FullName)
		if uerr != nil {
			return nil, uerr
		}
		targets = append(targets, cloneTarget{name: repo.Name, url: url})
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("geen repo's geselecteerd")
	}
	return targets, nil
}

// cloneOne cloneert één repo, of slaat hem over als de doelmap al bestaat.
// Fouten worden nooit teruggegeven maar als outcome vastgelegd: één kapotte
// repo mag een bulk-run van tientallen niet afbreken.
func (s *OrgSyncService) cloneOne(ctx context.Context, root string, t cloneTarget) domain.OrgCloneOutcome {
	dest := filepath.Join(root, t.name)
	out := domain.OrgCloneOutcome{Repo: t.name, Path: dest}

	if _, err := os.Lstat(dest); err == nil {
		out.Status = domain.OrgCloneSkipped
		out.Message = "map bestaat al"
		return out
	} else if !os.IsNotExist(err) {
		out.Status = domain.OrgCloneFailed
		out.Message = fmt.Sprintf("doelmap niet te controleren: %v", err)
		return out
	}

	repoCtx, cancel := context.WithTimeout(ctx, orgCloneRepoTimeout)
	defer cancel()

	if err := s.clone(repoCtx, root, t.url, t.name); err != nil {
		out.Status = domain.OrgCloneFailed
		out.Message = err.Error()
		return out
	}
	out.Status = domain.OrgCloneCloned
	return out
}

// cloneURL bouwt de ssh-remote uit owner/repo. Bewust ssh en niet https met
// token: alle bestaande checkouts in dit project gebruiken ssh, en de matching
// tussen org-repo en lokale map leest juist die origin-remote terug.
func cloneURL(fullName string) (string, error) {
	parts := strings.Split(strings.Trim(fullName, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("repo %q heeft geen bruikbare owner/repo-naam", fullName)
	}
	return fmt.Sprintf("git@github.com:%s/%s.git", parts[0], parts[1]), nil
}

// isSafeDirName weert alles wat als mapnaam buiten de projectmap zou kunnen
// wijzen of een verborgen map zou opleveren. GitHub laat deze namen niet toe,
// dus dit is een tweede slot op dezelfde deur.
func isSafeDirName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.HasPrefix(name, ".") {
		return false
	}
	return !strings.ContainsAny(name, `/\`) && filepath.Base(name) == name
}

func (s *OrgSyncService) emitClone(p domain.OrgCloneProgress) {
	if s.emitter == nil {
		return
	}
	s.emitter.Emit(orgCloneProgressEvent, p)
}

// defaultClone is de productie-implementatie van de clone-seam.
func defaultClone(ctx context.Context, parentDir, url, name string) error {
	return gitcli.Clone(ctx, parentDir, url, name)
}
