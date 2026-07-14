package services

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

func TestRenderReportHTML(t *testing.T) {
	r := domain.Report{
		ClientName:  "Cefetra",
		Period:      "Q2 2026",
		WebsiteName: "cefetra.nl",
		Acties:      []domain.ActieRow{{Actie: "Upgrade PHP", Wie: "Bitfactory"}},
		Monitoring:  []domain.MonitorRow{{Onderdeel: "Server Monitoring", Status: "✔ OK", Opmerking: ""}},
	}
	logoB64 := base64.StdEncoding.EncodeToString(reportLogoPNG)

	html, err := renderReportHTML(r, logoB64)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	for _, want := range []string{
		"Cefetra",
		"Q2 2026",
		"Servicecontract rapportage",
		"Acties en aandachtspunten",
		"Server, Uptime en TLS-monitoring",
		"Server software",
		"Managed software-updates",
		"AVG check",
		"Upgrade PHP",
		"cefetra.nl",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("expected rendered HTML to contain %q", want)
		}
	}
}

func TestRenderReportHTMLEmptyReportNoError(t *testing.T) {
	if _, err := renderReportHTML(domain.Report{}, ""); err != nil {
		t.Fatalf("render empty report: %v", err)
	}
}
