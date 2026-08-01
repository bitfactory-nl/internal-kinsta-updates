package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/browser"
	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
)

// crawlTimeout bounds a whole crawl. Pages are visited one at a time with lazy-load
// scrolling, so this is the wall-clock ceiling for maxPages pages.
const crawlTimeout = 20 * time.Minute

// crawlPageTimeout bounds one page load.
const crawlPageTimeoutMs = 20000

// CrawlScriptPath returns the crawl sidecar path, overridable via RDM_CRAWL.
func CrawlScriptPath() string {
	if p := os.Getenv("RDM_CRAWL"); p != "" {
		return p
	}
	return filepath.Join("sidecar", "crawl.mjs")
}

// siteCrawler is the subset of *browser.Crawler MediaService needs (test seam).
type siteCrawler interface {
	Crawl(ctx context.Context, req browser.CrawlRequest) (browser.CrawlResponse, error)
}

// MediaCrawlResult is what a rendered crawl established. The counts are the point:
// they say how much of the "no reference found" list is actually served to visitors,
// which is the one check a database scan cannot perform.
type MediaCrawlResult struct {
	ScanID           string              `json:"scanId"`
	SiteURL          string              `json:"siteUrl"`
	CrawledAt        time.Time           `json:"crawledAt"`
	PagesVisited     int                 `json:"pagesVisited"`
	PagesPlanned     int                 `json:"pagesPlanned"`
	UploadsSeen      int                 `json:"uploadsSeen"`
	UnreferencedSeen int                 `json:"unreferencedSeen"`
	Errors           []string            `json:"errors"`
	Pages            map[string][]string `json:"pages,omitempty"`
}

// CrawlSite visits the live site and records which uploads the browser really
// requests. Anything it finds is proof of use, so those files are excluded from
// quarantine from that moment on — regardless of what the database scan concluded.
func (s *MediaService) CrawlSite(projectID, envID, scanID string, maxPages int) (MediaCrawlResult, error) {
	p, err := s.project(projectID)
	if err != nil {
		return MediaCrawlResult{}, err
	}
	sum, err := s.store.Get(projectID, scanID)
	if err != nil {
		return MediaCrawlResult{}, err
	}
	if s.crawler == nil {
		return MediaCrawlResult{}, fmt.Errorf("crawler niet beschikbaar")
	}

	env := domain.EnvProd
	if tgt, _, terr := s.target(projectID, envID); terr == nil && tgt.EnvName != "" && tgt.EnvName != "live" {
		env = domain.EnvAcc
	}
	url, err := domain.ResolveEnvURL(p, env)
	if err != nil {
		return MediaCrawlResult{}, fmt.Errorf("geen URL voor omgeving %s: %w", env, err)
	}

	req := browser.CrawlRequest{BaseURL: url, MaxPages: maxPages, TimeoutMs: crawlPageTimeoutMs}
	if acc, aerr := config.ResolveTestAccess(p.Config.Testing, env); aerr == nil {
		req.BasicAuth = toBasic(acc)
	}

	slot := projectID + "@" + envID
	if !s.claim(slot) {
		return MediaCrawlResult{}, fmt.Errorf("er loopt al een actie voor deze omgeving")
	}
	defer s.release(slot)

	ctx, cancel := context.WithTimeout(context.Background(), crawlTimeout)
	defer cancel()

	resp, err := s.crawler.Crawl(ctx, req)
	if err != nil {
		return MediaCrawlResult{}, err
	}

	res := MediaCrawlResult{
		ScanID:       scanID,
		SiteURL:      url,
		CrawledAt:    s.now(),
		PagesVisited: resp.PagesVisited,
		PagesPlanned: resp.PagesPlanned,
		UploadsSeen:  len(resp.Uploads),
		Errors:       resp.Errors,
		Pages:        resp.Uploads,
	}

	// Hoeveel van de "ongebruikte" media wordt tóch geladen? Dat getal zegt in één
	// blik hoeveel de databasescan miste.
	if rijen, rerr := s.store.RowsForCategories(projectID, scanID, domain.MediaUnreferenced); rerr == nil {
		for pad := range rijen {
			if _, gezien := resp.Uploads[pad]; gezien {
				res.UnreferencedSeen++
			}
		}
	}
	_ = sum // de scan is alleen opgehaald om te bevestigen dat hij bestaat

	if err := s.store.SaveCrawl(projectID, scanID, res); err != nil {
		return res, err
	}
	s.vergeetCrawlCache(scanID)
	return res, nil
}

// CrawlSummary returns the stored crawl for a scan without the per-file page lists,
// so the UI can show the counts without pulling megabytes across the bridge.
func (s *MediaService) CrawlSummary(projectID, scanID string) (*MediaCrawlResult, error) {
	res, err := s.store.GetCrawl(projectID, scanID)
	if err != nil || res == nil {
		return nil, err
	}
	kort := *res
	kort.Pages = nil
	return &kort, nil
}

// FileUsage returns the pages where a crawl saw one file. Empty means: not seen on
// any page this crawl visited — which is not the same as unused, because a crawl only
// covers the pages it reached.
func (s *MediaService) FileUsage(projectID, scanID, path string) ([]string, error) {
	res, err := s.store.GetCrawl(projectID, scanID)
	if err != nil || res == nil {
		return nil, err
	}
	return res.Pages[path], nil
}

// crawlPaden geeft de padenset van een crawl, met een kleine cache: de UI vraagt
// detailregels per pagina op en dan zou hetzelfde bestand steeds opnieuw worden
// gelezen.
func (s *MediaService) crawlPaden(projectID, scanID string) map[string]bool {
	s.crawlMu.Lock()
	defer s.crawlMu.Unlock()
	if s.crawlCache == nil {
		s.crawlCache = map[string]map[string]bool{}
	}
	if set, ok := s.crawlCache[scanID]; ok {
		return set
	}
	set := map[string]bool{}
	if res, err := s.store.GetCrawl(projectID, scanID); err == nil && res != nil {
		for pad := range res.Pages {
			set[pad] = true
		}
	}
	s.crawlCache[scanID] = set
	return set
}

func (s *MediaService) vergeetCrawlCache(scanID string) {
	s.crawlMu.Lock()
	delete(s.crawlCache, scanID)
	s.crawlMu.Unlock()
}

// MediaCrawlConflict is een bestand waarover de twee methodes het oneens zijn: de
// databasescan vond geen enkele verwijzing, maar de browser vroeg het wél op. Dat is
// geen foutmelding maar een controlelijst — elke regel is met één klik na te lopen.
type MediaCrawlConflict struct {
	Path         string   `json:"path"`
	Bytes        int64    `json:"bytes"`
	ModifiedAt   int64    `json:"modifiedAt"`
	AttachmentID int      `json:"attachmentId,omitempty"`
	Title        string   `json:"title,omitempty"`
	Pages        []string `json:"pages"`
}

// CrawlConflicts lists the files the scan called unreferenced while the crawl saw
// them being loaded, largest first. These are the files that would have been moved
// on the database scan alone, so they deserve to be checked by hand.
func (s *MediaService) CrawlConflicts(projectID, scanID string, offset, limit int) ([]MediaCrawlConflict, error) {
	if limit <= 0 || limit > 2000 {
		limit = 200
	}
	crawl, err := s.store.GetCrawl(projectID, scanID)
	if err != nil || crawl == nil {
		return nil, err
	}
	rijen, err := s.store.RowsForCategories(projectID, scanID, domain.MediaUnreferenced)
	if err != nil {
		return nil, err
	}

	uit := make([]MediaCrawlConflict, 0, len(crawl.Pages))
	for pad, paginas := range crawl.Pages {
		rij, ok := rijen[pad]
		if !ok {
			continue // de scan noemde dit al in gebruik; geen tegenspraak
		}
		uit = append(uit, MediaCrawlConflict{
			Path:         pad,
			Bytes:        rij.Bytes,
			ModifiedAt:   rij.ModifiedAt,
			AttachmentID: rij.AttachmentID,
			Title:        rij.Title,
			Pages:        paginas,
		})
	}

	// Grootste eerst: die wegen het zwaarst als je ze ten onrechte zou verplaatsen.
	// Bij gelijke grootte op pad, zodat de volgorde tussen aanroepen stabiel is en
	// pagineren betrouwbaar werkt.
	sort.Slice(uit, func(i, j int) bool {
		if uit[i].Bytes != uit[j].Bytes {
			return uit[i].Bytes > uit[j].Bytes
		}
		return uit[i].Path < uit[j].Path
	})

	if offset >= len(uit) {
		return []MediaCrawlConflict{}, nil
	}
	eind := offset + limit
	if eind > len(uit) {
		eind = len(uit)
	}
	return uit[offset:eind], nil
}
