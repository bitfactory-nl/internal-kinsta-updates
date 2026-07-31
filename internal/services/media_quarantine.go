package services

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rdm/sites-tool/internal/domain"
)

//go:embed media_quarantine.php
var mediaQuarantineScript string

const (
	quarantineSentinelStart = "<<<RDM-QUARANTINE-1>>>"
	quarantineSentinelEnd   = "<<<END-RDM-QUARANTINE-1>>>"
)

// quarantineTimeout bounds one move or restore. Moving files is a rename on the same
// filesystem, so this is fast even for thousands of files.
const quarantineTimeout = 10 * time.Minute

// quarantineMinAgeDays is the default floor: media younger than this is never moved
// unless the caller lowers it on purpose. Fresh uploads are the most likely to be
// referenced from somewhere the scan cannot see.
const quarantineMinAgeDays = 90

// QuarantineEntry is one moved file.
type QuarantineEntry struct {
	Original string `json:"original"`
	Stored   string `json:"stored"`
	Bytes    int64  `json:"bytes"`
}

// QuarantineSkip is one file that was left alone, with the reason.
type QuarantineSkip struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// QuarantineBatch is one set of moved files, as recorded on the server.
type QuarantineBatch struct {
	Batch   string `json:"batch"`
	Created string `json:"created"`
	Files   int    `json:"files"`
	Bytes   int64  `json:"bytes"`
}

// QuarantineResult is the outcome of a move or restore.
type QuarantineResult struct {
	Batch         string            `json:"batch"`
	Moved         []QuarantineEntry `json:"moved"`
	Skipped       []QuarantineSkip  `json:"skipped"`
	Bytes         int64             `json:"bytes"`
	QuarantineDir string            `json:"quarantineDir"`
	Batches       []QuarantineBatch `json:"batches"`
	Error         string            `json:"error"`
}

// quarantineOpdracht is what the server-side script receives.
type quarantineOpdracht struct {
	Action string   `json:"action"`
	Batch  string   `json:"batch,omitempty"`
	Paths  []string `json:"paths,omitempty"`
}

// buildQuarantineCommand wraps one quarantine action in a single shell command. The
// quarantine directory lives next to the webroot, not inside uploads: the files must
// become unreachable (so broken links show up) and must not turn up as strays in the
// next scan.
func buildQuarantineCommand(webroot string, opdracht quarantineOpdracht) (string, error) {
	data, err := json.Marshal(opdracht)
	if err != nil {
		return "", fmt.Errorf("opdracht serialiseren: %w", err)
	}
	return strings.Join([]string{
		zoekWebroot(webroot),
		`if [ -z "$root" ] || [ ! -f "$root/wp-config.php" ]; then echo "RDM-ERR:geen wp-config.php gevonden"; exit 3; fi`,
		`cd "$root" || exit 3`,
		`echo "RDM-ROOT:$root"`,
		fmt.Sprintf(`printf %%s '%s' | base64 -d | RDM_Q_BASE="$HOME/rdm-quarantine" RDM_Q_INPUT=%s nice -n 19 wp eval-file - 2>&1`,
			base64.StdEncoding.EncodeToString([]byte(mediaQuarantineScript)),
			base64.StdEncoding.EncodeToString(data)),
	}, "\n"), nil
}

func parseQuarantineOutput(stdout string) (QuarantineResult, error) {
	raw, err := pakSentinel(stdout, quarantineSentinelStart, quarantineSentinelEnd)
	if err != nil {
		return QuarantineResult{}, err
	}
	var r QuarantineResult
	if err := json.Unmarshal(raw, &r); err != nil {
		return QuarantineResult{}, fmt.Errorf("resultaat parsen: %w", err)
	}
	if r.Error != "" {
		return r, fmt.Errorf("quarantaine op de server: %s", r.Error)
	}
	return r, nil
}

// veiligeCategorieën are the only categories a file may be moved from: media the
// site references, and library entries whose file is already gone, are never touched.
var veiligeCategorieën = []domain.MediaCategory{domain.MediaUnreferenced, domain.MediaOrphanFile}

// controleerScanGeschikt refuses to act on a scan that cannot support the decision.
// Every one of these gates exists because the alternative is moving a file that is
// actually in use.
func controleerScanGeschikt(sum domain.MediaScanSummary) error {
	if sum.Scope.Truncated {
		return fmt.Errorf("deze scan is afgekapt; draai eerst een volledige scan")
	}
	if !sum.Scope.ReferenceScanComplete {
		return fmt.Errorf("de referentiescan van deze scan is niet afgerond; \"geen referentie gevonden\" is dan geen bevinding")
	}
	if sum.Scope.OffloadDetected {
		return fmt.Errorf("deze site gebruikt een offload-plugin; de bestandslijst zegt dan te weinig over wat er echt staat")
	}
	return nil
}

// QuarantineFiles moves the given files out of uploads into a quarantine directory
// and records a manifest so the whole batch can be put back. Nothing is deleted, and
// the attachment records in WordPress stay untouched — that is what makes a restore a
// matter of moving files back.
//
// A path is only accepted when the stored scan itself placed it in a safe category.
// That check is the real safety net: it means neither a UI bug nor a stale selection
// can reach a file the scan found in use.
func (s *MediaService) QuarantineFiles(projectID, envID, scanID string, paths []string, minAgeDays int) (QuarantineResult, error) {
	if len(paths) == 0 {
		return QuarantineResult{}, fmt.Errorf("geen bestanden geselecteerd")
	}
	sum, err := s.store.Get(projectID, scanID)
	if err != nil {
		return QuarantineResult{}, err
	}
	if err := controleerScanGeschikt(sum); err != nil {
		return QuarantineResult{}, err
	}
	if minAgeDays < 0 {
		minAgeDays = quarantineMinAgeDays
	}

	toegestaan, err := s.store.RowsForCategories(projectID, scanID, veiligeCategorieën...)
	if err != nil {
		return QuarantineResult{}, err
	}

	grens := s.now().AddDate(0, 0, -minAgeDays).Unix()
	var teVerplaatsen []string
	var geweigerd []QuarantineSkip
	for _, pad := range paths {
		rij, ok := toegestaan[pad]
		if !ok {
			geweigerd = append(geweigerd, QuarantineSkip{
				Path:   pad,
				Reason: "staat in deze scan niet als ongebruikt of zwerfbestand",
			})
			continue
		}
		if minAgeDays > 0 && rij.ModifiedAt > grens {
			geweigerd = append(geweigerd, QuarantineSkip{
				Path:   pad,
				Reason: fmt.Sprintf("jonger dan %d dagen", minAgeDays),
			})
			continue
		}
		teVerplaatsen = append(teVerplaatsen, pad)
	}
	if len(teVerplaatsen) == 0 {
		return QuarantineResult{Skipped: geweigerd}, fmt.Errorf("geen van de geselecteerde bestanden komt in aanmerking")
	}

	res, err := s.voerQuarantaineUit(projectID, envID, quarantineOpdracht{
		Action: "quarantine",
		Batch:  s.now().Format("20060102-150405"),
		Paths:  teVerplaatsen,
	})
	res.Skipped = append(res.Skipped, geweigerd...)
	return res, err
}

// RestoreQuarantine puts a whole batch back where it came from. Files whose original
// place is occupied again are left in quarantine and reported, never overwritten.
func (s *MediaService) RestoreQuarantine(projectID, envID, batch string) (QuarantineResult, error) {
	if strings.TrimSpace(batch) == "" {
		return QuarantineResult{}, fmt.Errorf("geen batch opgegeven")
	}
	return s.voerQuarantaineUit(projectID, envID, quarantineOpdracht{Action: "restore", Batch: batch})
}

// ListQuarantine returns the batches currently in quarantine on the server.
func (s *MediaService) ListQuarantine(projectID, envID string) ([]QuarantineBatch, error) {
	res, err := s.voerQuarantaineUit(projectID, envID, quarantineOpdracht{Action: "list"})
	if err != nil {
		return nil, err
	}
	return res.Batches, nil
}

func (s *MediaService) voerQuarantaineUit(projectID, envID string, opdracht quarantineOpdracht) (QuarantineResult, error) {
	tgt, _, err := s.target(projectID, envID)
	if err != nil {
		return QuarantineResult{}, err
	}

	// Dezelfde slot-sleutel als de scan: een scan en een verplaatsing tegelijk op
	// dezelfde omgeving zouden elkaars beeld van de bestanden ondermijnen.
	slot := projectID + "@" + envID
	if !s.claim(slot) {
		return QuarantineResult{}, fmt.Errorf("er loopt al een actie voor deze omgeving")
	}
	defer s.release(slot)

	cmd, err := buildQuarantineCommand(tgt.Webroot, opdracht)
	if err != nil {
		return QuarantineResult{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), quarantineTimeout)
	defer cancel()

	out, runErr := s.ssh.RunCommand(ctx, tgt.SSH, cmd)
	res, perr := parseQuarantineOutput(out)
	if perr != nil {
		if melding := eersteGroep(reMediaErr, out); melding != "" {
			return QuarantineResult{}, fmt.Errorf("op de server: %s", melding)
		}
		if runErr != nil {
			return QuarantineResult{}, fmt.Errorf("quarantaine op %s: %w", tgt.SSH.Host, runErr)
		}
		return QuarantineResult{}, perr
	}
	return res, nil
}
