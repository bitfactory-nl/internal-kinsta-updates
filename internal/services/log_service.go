package services

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rdm/sites-tool/internal/domain"
)

// logFetchTimeout bounds one log retrieval. A 20.000-line file is several
// megabytes, so this is generous compared to the other Kinsta calls.
const logFetchTimeout = 2 * time.Minute

// kinstaLogSource fetches raw log contents (test seam).
type kinstaLogSource interface {
	EnvironmentLogs(ctx context.Context, envID, fileName string, lines int) (string, error)
}

// LogService fetches Kinsta's per-environment log files and turns them into
// something a human can act on: grouped, classified and with a verdict per group
// on whether an AI fix attempt makes sense.
//
// It is read-only towards Kinsta; the only thing it changes is its own cache.
type LogService struct {
	projects *ProjectService
	kinsta   kinstaLogSource
	now      func() time.Time

	mu      sync.Mutex
	laatste map[string]domain.LogFetchResult
}

func NewLogService(projects *ProjectService, kinsta kinstaLogSource) *LogService {
	return &LogService{
		projects: projects,
		kinsta:   kinsta,
		now:      time.Now,
		laatste:  map[string]domain.LogFetchResult{},
	}
}

func (s *LogService) project(id string) (domain.Project, error) {
	p, ok := s.projects.Get(id)
	if !ok {
		return domain.Project{}, fmt.Errorf("project %q niet gevonden", id)
	}
	return p, nil
}

// Fetch retrieves one log file for an environment and returns it grouped.
func (s *LogService) Fetch(projectID, envID, file string, lines int) (domain.LogFetchResult, error) {
	logFile := domain.LogFile(file)
	if !logFile.Valid() {
		return domain.LogFetchResult{}, fmt.Errorf("onbekend logbestand %q", file)
	}
	p, err := s.project(projectID)
	if err != nil {
		return domain.LogFetchResult{}, err
	}
	if envID == "" {
		return domain.LogFetchResult{}, fmt.Errorf("kies eerst een omgeving")
	}

	ctx, cancel := context.WithTimeout(context.Background(), logFetchTimeout)
	defer cancel()

	raw, err := s.kinsta.EnvironmentLogs(ctx, envID, string(logFile), lines)
	if err != nil {
		return domain.LogFetchResult{}, fmt.Errorf("logs ophalen bij Kinsta: %w", err)
	}

	entries := parseLogFile(logFile, raw)
	groepen := groepeerEntries(entries)

	res := domain.LogFetchResult{
		File:           logFile,
		Environment:    envID,
		LinesRequested: lines,
		LinesReceived:  aantalRegels(raw),
		Parsed:         len(entries),
		Groups:         make([]domain.LogGroup, 0, len(groepen)),
		FetchedAt:      s.now(),
	}
	for _, g := range groepen {
		res.Groups = append(res.Groups, verrijkGroep(g, p.Path))
	}
	res.Warnings = fetchWaarschuwingen(res)

	s.mu.Lock()
	s.laatste[cacheSleutel(projectID, logFile)] = res
	s.mu.Unlock()

	return res, nil
}

// verrijkGroep resolves the group's production path against the checkout and
// decides whether an AI fix attempt is worth starting.
func verrijkGroep(g domain.LogGroup, repoRoot string) domain.LogGroup {
	if g.File != "" {
		bestand := mapProdPathToRepo(g.File, repoRoot)
		g.RepoPath = bestand.RepoPad
		g.IsCore = bestand.IsCore
	}
	for _, sample := range g.Samples {
		if scrubVoorAI(sample.Raw).HeeftPII() {
			g.HasPII = true
			break
		}
	}
	g.AIEligible, g.AIReason = beoordeelAIKandidaat(g)
	return g
}

// beoordeelAIKandidaat is de poort voor de AI-knop. De reden wordt altijd
// gevuld, ook bij een 'ja': een uitgeschakelde knop zonder uitleg is in de UI
// erger dan geen knop.
//
// De regels komen uit wat een echt log liet zien. Verreweg de meeste regels zijn
// bots die mappen aftasten, en zelfs de PHP-fatals waren bots die een
// core-bestand rechtstreeks opvroegen. Een AI daarop af sturen kost tijd en
// tokens en levert per definitie niets op.
func beoordeelAIKandidaat(g domain.LogGroup) (bool, string) {
	switch {
	case g.Kind == domain.KindBotProbe:
		return false, "botverkeer: een scanner die mappen aftast. Hier valt in de code niets te repareren."
	case g.Kind == domain.KindAccess:
		return false, "een access-regel, geen fout."
	case !g.Kind.IsPHP():
		return false, "geen PHP-fout maar een melding van de webserver zelf; dit is meestal configuratie of een ontbrekend bestand."
	case g.File == "":
		return false, "de melding noemt geen bestand, dus er is geen plek in de code om naar te kijken."
	case g.RepoPath == "":
		return false, fmt.Sprintf("%s staat niet in deze checkout — waarschijnlijk een plugin of thema dat niet in git zit. Pull eerst, of los het bij de leverancier op.", g.File)
	case g.IsCore:
		return false, fmt.Sprintf("%s is een WordPress core-bestand. Een wijziging daar wordt bij de volgende core-update overschreven; zo'n melding komt meestal van een bot die het bestand direct opvraagt.", g.RepoPath)
	}
	return true, fmt.Sprintf("PHP-fout in projectcode: %s:%d", g.RepoPath, g.Line)
}

func fetchWaarschuwingen(res domain.LogFetchResult) []string {
	var uit []string
	if res.LinesReceived == 0 {
		uit = append(uit, "het logbestand is leeg — dat is bij een rustige site normaal")
		return uit
	}
	if res.LinesRequested > 0 && res.LinesReceived >= res.LinesRequested {
		uit = append(uit, fmt.Sprintf("Kinsta gaf precies %d regels terug, het gevraagde maximum; er is waarschijnlijk meer historie dan hier staat", res.LinesReceived))
	}
	var ruis, kandidaten int
	for _, g := range res.Groups {
		if g.Kind == domain.KindBotProbe {
			ruis += g.Count
		}
		if g.AIEligible {
			kandidaten++
		}
	}
	if ruis > 0 && res.Parsed > 0 {
		uit = append(uit, fmt.Sprintf("%d van de %d regels is botverkeer (%d%%)", ruis, res.Parsed, ruis*100/res.Parsed))
	}
	if kandidaten == 0 && res.File == domain.LogFileError {
		uit = append(uit, "geen enkele melding wijst naar code in deze checkout; er is dus niets voor de AI om op te pakken")
	}
	return uit
}

// GroupByID returns a group from the last fetch for this project.
func (s *LogService) GroupByID(projectID, groupID string) (domain.LogGroup, domain.LogFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, file := range []domain.LogFile{domain.LogFileError, domain.LogFileAccess, domain.LogFileCachePerf} {
		res, ok := s.laatste[cacheSleutel(projectID, file)]
		if !ok {
			continue
		}
		for _, g := range res.Groups {
			if g.ID == groupID {
				return g, file, nil
			}
		}
	}
	return domain.LogGroup{}, "", fmt.Errorf("melding %q niet gevonden; haal de logs opnieuw op", groupID)
}

// FixPreview shows exactly what would be sent to the AI, so it can be read
// before anything leaves the machine.
func (s *LogService) FixPreview(projectID, groupID string) (domain.AIFixPreview, error) {
	g, _, err := s.GroupByID(projectID, groupID)
	if err != nil {
		return domain.AIFixPreview{}, err
	}
	prompt, gemaskeerd := bouwFixPrompt(g)
	return domain.AIFixPreview{
		GroupID:  g.ID,
		Prompt:   prompt,
		Masked:   gemaskeerd,
		RepoPath: g.RepoPath,
		Branch:   fixBranchNaam(g),
	}, nil
}

func cacheSleutel(projectID string, file domain.LogFile) string {
	return projectID + "|" + string(file)
}

func aantalRegels(raw string) int {
	if strings.TrimSpace(raw) == "" {
		return 0
	}
	n := 0
	for _, r := range strings.Split(raw, "\n") {
		if strings.TrimSpace(r) != "" {
			n++
		}
	}
	return n
}
