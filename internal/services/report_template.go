package services

import (
	_ "embed"
	"fmt"
	"html/template"
	"strings"

	"github.com/rdm/sites-tool/internal/domain"
)

//go:embed report_template.html
var reportTemplateSrc string

//go:embed report_logo.png
var reportLogoPNG []byte

// reportView is the data passed to report_template.html: the domain.Report
// fields plus the base64-encoded logo (kept out of domain, which is
// presentation-agnostic).
type reportView struct {
	domain.Report
	LogoB64 string
}

// renderReportHTML renders r into the Bitfactory house-style report HTML.
// logoB64 is the base64-encoded (no data: prefix) Bitfactory logo PNG.
func renderReportHTML(r domain.Report, logoB64 string) (string, error) {
	tmpl, err := template.New("report").Parse(reportTemplateSrc)
	if err != nil {
		return "", fmt.Errorf("parse report template: %w", err)
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, reportView{Report: r, LogoB64: logoB64}); err != nil {
		return "", fmt.Errorf("render report template: %w", err)
	}
	return buf.String(), nil
}
