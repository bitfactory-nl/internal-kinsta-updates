package config

import (
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

func sampleFlows() []domain.Flow {
	return []domain.Flow{{
		Name: "Contactformulier",
		Steps: []domain.Step{
			{Action: domain.StepNavigate, Target: "/"},
			{Action: domain.StepClick, Target: "Cookies accepteren"},
			{Action: domain.StepInput, Target: "E-mail", Value: "test@example.com"},
			{Action: domain.StepAssert, Target: "Bedankt-bericht zichtbaar"},
		},
	}}
}

func TestFlowsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := SaveFlows(dir, sampleFlows()); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadFlows(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Contactformulier" || len(got[0].Steps) != 4 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if got[0].Steps[2].Value != "test@example.com" {
		t.Errorf("value lost: %+v", got[0].Steps[2])
	}
}

func TestLoadFlowsMissingFile(t *testing.T) {
	got, err := LoadFlows(t.TempDir())
	if err != nil {
		t.Fatalf("expected nil error for missing file, got %v", err)
	}
	if got != nil {
		t.Errorf("expected nil flows, got %+v", got)
	}
}

func TestValidateFlows(t *testing.T) {
	cases := []struct {
		name    string
		flows   []domain.Flow
		wantErr bool
	}{
		{"ok", sampleFlows(), false},
		{"empty name", []domain.Flow{{Name: "", Steps: sampleFlows()[0].Steps}}, true},
		{"no steps", []domain.Flow{{Name: "X"}}, true},
		{"bad action", []domain.Flow{{Name: "X", Steps: []domain.Step{{Action: "boop", Target: "/"}}}}, true},
		{"type without value", []domain.Flow{{Name: "X", Steps: []domain.Step{{Action: domain.StepInput, Target: "E-mail"}}}}, true},
		{"missing target", []domain.Flow{{Name: "X", Steps: []domain.Step{{Action: domain.StepNavigate}}}}, true},
		{"duplicate name", []domain.Flow{sampleFlows()[0], sampleFlows()[0]}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateFlows(c.flows)
			if (err != nil) != c.wantErr {
				t.Errorf("ValidateFlows() err = %v, wantErr %v", err, c.wantErr)
			}
		})
	}
}
