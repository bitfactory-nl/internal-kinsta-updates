package services

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/kinsta"
	"github.com/rdm/sites-tool/internal/domain"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// reportProjects is the subset of *ProjectService ReportService needs (test seam).
type reportProjects interface {
	Get(id string) (domain.Project, bool)
}

// reportKinsta is the subset of *KinstaService ReportService needs (test seam).
type reportKinsta interface {
	GetSiteDetails(siteID string) (*kinsta.SiteDetails, error)
	GetEnvironmentPluginsAndThemes(envID string) (*kinsta.EnvironmentDetails, error)
}

// reportSecurity is the subset of *SecurityService ReportService needs (test seam).
type reportSecurity interface {
	GetScanResults(projectID string) (*SecurityScanResult, error)
}

// reportPDF is the subset of *browser.PDFRunner ReportService needs (test seam).
type reportPDF interface {
	RenderPDF(ctx context.Context, html, outPath string) error
}

// ReportService builds, persists and exports the per-project client report
// ("Servicecontract rapportage", Bitfactory house style).
type ReportService struct {
	projects reportProjects
	kinsta   reportKinsta
	security reportSecurity
	store    *ReportStore
	pdf      reportPDF
	app      *application.App
}

// NewReportService wires the service.
func NewReportService(projects reportProjects, kinsta reportKinsta, security reportSecurity, store *ReportStore, pdf reportPDF) *ReportService {
	return &ReportService{projects: projects, kinsta: kinsta, security: security, store: store, pdf: pdf}
}

// SetApp injects the Wails app reference (called after app creation), needed
// for the native save-file dialog in ExportPDF.
func (s *ReportService) SetApp(app *application.App) {
	s.app = app
}

// PDFScriptPath returns the pdf.mjs sidecar path, overridable via
// RDM_PDF_SIDECAR (mirrors SidecarScriptPath/RDM_SIDECAR).
func PDFScriptPath() string {
	if p := os.Getenv("RDM_PDF_SIDECAR"); p != "" {
		return p
	}
	return filepath.Join("sidecar", "pdf.mjs")
}

// DefaultReportsDir is ~/.config/rdm/reports.
func DefaultReportsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "rdm", "reports")
}

// GetReport returns the stored draft for projectID+period, or a fresh
// skeleton (default rows, prefilled client/website name) if none exists yet.
func (s *ReportService) GetReport(projectID, period string) (domain.Report, error) {
	p, ok := s.projects.Get(projectID)
	if !ok {
		return domain.Report{}, fmt.Errorf("project %q niet gevonden", projectID)
	}

	stored, err := s.store.Get(projectID, period)
	if err != nil {
		return domain.Report{}, err
	}
	if stored.ProjectID == "" {
		return skeletonReport(p, projectID, period), nil
	}
	return stored, nil
}

// skeletonReport builds a fresh draft with the default rows described in the
// house-style docx and client/website name prefilled from the project.
func skeletonReport(p domain.Project, projectID, period string) domain.Report {
	return domain.Report{
		ProjectID:   projectID,
		Period:      period,
		ClientName:  p.DisplayName,
		WebsiteName: hostnameOf(p.Deploy.Link.Prod),
		Monitoring: []domain.MonitorRow{
			{Onderdeel: "Server Monitoring", Status: "✔ OK"},
			{Onderdeel: "Uptime Monitoring", Status: "✔ OK"},
			{Onderdeel: "TLS-certificaat", Status: "✔ OK"},
		},
		Software: []domain.SoftwareRow{
			{Component: "PHP"},
			{Component: "MariaDB"},
			{Component: "Node"},
			{Component: "WordPress"},
		},
		DependencyUpdates: []domain.UpdateRow{
			{Naam: "Composer - PHP packages"},
			{Naam: "NPM - Frontend packages"},
		},
		WPUpdates: []domain.UpdateRow{
			{Naam: "WordPress"},
			{Naam: "WordPress plug-ins"},
		},
	}
}

// hostnameOf extracts the host from a URL for the "website" label. Falls
// back to the raw (trimmed) input if it doesn't parse as a URL.
func hostnameOf(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	parseable := rawURL
	if !strings.Contains(parseable, "://") {
		parseable = "https://" + parseable
	}
	u, err := url.Parse(parseable)
	if err != nil || u.Host == "" {
		return rawURL
	}
	return u.Host
}

// prodEnvID picks the Kinsta environment ID to use as the source for
// prefill data. Heuristic: prefer the map key "prod" or "production"
// (case-insensitive); otherwise fall back to the alphabetically-first key so
// the choice is deterministic across runs (Go map iteration order is not).
func prodEnvID(p domain.Project) string {
	if p.Config.Kinsta == nil || len(p.Config.Kinsta.Environments) == 0 {
		return ""
	}
	envs := p.Config.Kinsta.Environments
	for key, binding := range envs {
		lk := strings.ToLower(key)
		if lk == "prod" || lk == "production" {
			return binding.EnvID
		}
	}
	keys := make([]string, 0, len(envs))
	for k := range envs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return envs[keys[0]].EnvID
}

// Prefill starts from the stored (or skeleton) draft and fills in fields from
// live Kinsta + security-scan data. Each source is best-effort: an error from
// Kinsta or the security scan is skipped (non-fatal) so the rest of the
// prefill still runs. Does NOT save the result.
func (s *ReportService) Prefill(projectID, period string) (domain.Report, error) {
	r, err := s.GetReport(projectID, period)
	if err != nil {
		return domain.Report{}, err
	}
	p, ok := s.projects.Get(projectID)
	if !ok {
		return domain.Report{}, fmt.Errorf("project %q niet gevonden", projectID)
	}

	s.prefillFromKinsta(&r, p)
	s.prefillFromSecurity(&r, projectID)

	return r, nil
}

func (s *ReportService) prefillFromKinsta(r *domain.Report, p domain.Project) {
	if s.kinsta == nil || p.Config.Kinsta == nil {
		return
	}
	envID := prodEnvID(p)
	if envID == "" {
		return
	}

	if details, err := s.kinsta.GetSiteDetails(p.Config.Kinsta.SiteID); err == nil && details != nil {
		for _, e := range details.Environments {
			if e.ID != envID {
				continue
			}
			setSoftwareHuidig(r, "PHP", e.ContainerInfo.PHPEngineVersion)
			setSoftwareHuidig(r, "WordPress", e.WordPressVersion)
			break
		}
	}

	epd, err := s.kinsta.GetEnvironmentPluginsAndThemes(envID)
	if err != nil || epd == nil {
		return
	}
	pluginUpdates, vulnerable := 0, 0
	for _, pl := range epd.Plugins {
		if pl.UpdateVersion != "" {
			pluginUpdates++
		}
		if pl.IsVersionVulnerable {
			vulnerable++
		}
	}
	setUpdateOpmerking(r.WPUpdates, "WordPress plug-ins", fmt.Sprintf("%d plugin-updates beschikbaar", pluginUpdates))
	if vulnerable > 0 {
		r.Acties = appendActieIfAbsent(r.Acties, domain.ActieRow{
			Actie: fmt.Sprintf("%d kwetsbare plugin(s) gevonden", vulnerable),
			Wie:   "Bitfactory",
		})
	}
}

func (s *ReportService) prefillFromSecurity(r *domain.Report, projectID string) {
	if s.security == nil {
		return
	}
	sec, err := s.security.GetScanResults(projectID)
	if err != nil || sec == nil || len(sec.Findings) == 0 {
		return
	}
	var npm, composer int
	for _, f := range sec.Findings {
		switch f.Source {
		case "npm":
			npm++
		case "composer":
			composer++
		}
	}
	r.Acties = appendActieIfAbsent(r.Acties, domain.ActieRow{
		Actie: fmt.Sprintf("%d security-findings uit npm/composer audit", len(sec.Findings)),
		Wie:   "Bitfactory",
	})
	setUpdateOpmerking(r.DependencyUpdates, "Composer - PHP packages", fmt.Sprintf("%d findings", composer))
	setUpdateOpmerking(r.DependencyUpdates, "NPM - Frontend packages", fmt.Sprintf("%d findings", npm))
}

// actieGeneratedMarkers are the stable substrings identifying a
// Prefill-generated ActieRow "kind". The leading count in the Actie text
// changes between runs (e.g. "1 kwetsbare..." -> "2 kwetsbare..."), so
// matching is done on the fixed tail rather than the full string.
var actieGeneratedMarkers = []string{"kwetsbare plugin", "security-finding"}

// actieMatch reports whether existing is a previously generated instance of
// the same kind as candidate (same "Wie" + same stable marker substring).
func actieMatch(existing, candidate domain.ActieRow) bool {
	if existing.Wie != candidate.Wie {
		return false
	}
	for _, marker := range actieGeneratedMarkers {
		if strings.Contains(candidate.Actie, marker) {
			return strings.Contains(existing.Actie, marker)
		}
	}
	return existing.Actie == candidate.Actie
}

// appendActieIfAbsent appends row to rows, unless a previously generated row
// of the same kind (per actieMatch) already exists — in which case that row
// is overwritten in place with the fresh text. This keeps repeated
// Prefill -> SaveReport -> Prefill cycles idempotent instead of accumulating
// duplicate rows with stale counts.
func appendActieIfAbsent(rows []domain.ActieRow, row domain.ActieRow) []domain.ActieRow {
	for i := range rows {
		if actieMatch(rows[i], row) {
			rows[i] = row
			return rows
		}
	}
	return append(rows, row)
}

func setSoftwareHuidig(r *domain.Report, component, version string) {
	if version == "" {
		return
	}
	for i := range r.Software {
		if r.Software[i].Component == component {
			r.Software[i].Huidig = version
			return
		}
	}
}

func setUpdateOpmerking(rows []domain.UpdateRow, naam, opmerking string) {
	for i := range rows {
		if rows[i].Naam == naam {
			rows[i].Opmerking = opmerking
			return
		}
	}
}

// SaveReport persists r, stamping UpdatedAt.
func (s *ReportService) SaveReport(r domain.Report) error {
	r.UpdatedAt = time.Now()
	return s.store.Save(r)
}

// ListReports returns the stored report drafts for a project, newest first.
func (s *ReportService) ListReports(projectID string) ([]domain.Report, error) {
	return s.store.List(projectID)
}

// ExportPDF renders the stored report for projectID+period to HTML, asks the
// user (via native save dialog) where to save it, then renders it to PDF via
// the Playwright sidecar. Returns "" (no error) if the user cancels.
func (s *ReportService) ExportPDF(projectID, period string) (string, error) {
	if s.app == nil {
		return "", fmt.Errorf("app not initialized")
	}

	r, err := s.GetReport(projectID, period)
	if err != nil {
		return "", err
	}

	logoB64 := base64.StdEncoding.EncodeToString(reportLogoPNG)
	html, err := renderReportHTML(r, logoB64)
	if err != nil {
		return "", err
	}

	filename := fmt.Sprintf("Servicecontract rapportage - %s - %s.pdf", r.ClientName, r.Period)
	chosen, err := s.app.Dialog.SaveFile().
		SetMessage("Exporteer rapportage als PDF").
		SetFilename(filename).
		CanCreateDirectories(true).
		PromptForSingleSelection()
	if err != nil {
		return "", fmt.Errorf("dialog: %w", err)
	}
	if chosen == "" {
		return "", nil // user cancelled
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := s.pdf.RenderPDF(ctx, html, chosen); err != nil {
		return "", fmt.Errorf("render pdf: %w", err)
	}
	return chosen, nil
}
