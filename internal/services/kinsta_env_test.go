package services

import (
	"testing"

	"github.com/rdm/sites-tool/internal/adapters/kinsta"
)

// De omgeving die bij de site is gekozen (KinstaEnvID) is de standaard.
func TestKinstaEnvIDVanDeSiteIsStandaard(t *testing.T) {
	ps, web := siteMetKinstaFixture(t)
	p, _ := ps.Get(projectID(web))
	if p.KinstaEnvID != "e-2" {
		t.Fatalf("KinstaEnvID %q", p.KinstaEnvID)
	}
	svc := NewKinstaService(nil, ps)
	id, err := svc.LinkedEnvID(projectID(web))
	if err != nil || id != "e-2" {
		t.Fatalf("LinkedEnvID %q %v", id, err)
	}
	envs := []kinsta.Environment{{ID: "e-1", Name: "live"}, {ID: "e-2", Name: "staging"}}
	env, err := kiesOmgeving(envs, "", id)
	if err != nil || env.ID != "e-2" {
		t.Fatalf("keuze %+v %v", env, err)
	}
}

func TestKiesOmgeving(t *testing.T) {
	envs := []kinsta.Environment{{ID: "e-0", Name: "staging"}, {ID: "e-1", Name: "live"}, {ID: "e-2", Name: "dev"}}
	gevallen := []struct {
		naam, gekozen, voorkeur, want string
	}{
		{"expliciete keuze wint", "e-0", "e-2", "e-0"},
		{"voorkeur van de site", "", "e-2", "e-2"},
		{"onbekende voorkeur valt terug op live", "", "weg", "e-1"},
		{"zonder voorkeur live", "", "", "e-1"},
	}
	for _, g := range gevallen {
		env, err := kiesOmgeving(envs, g.gekozen, g.voorkeur)
		if err != nil || env.ID != g.want {
			t.Fatalf("%s: %+v %v", g.naam, env, err)
		}
	}
	if _, err := kiesOmgeving(envs, "weg", ""); err == nil {
		t.Fatal("onbekende expliciete keuze hoort te falen")
	}
	if env, _ := kiesOmgeving(envs[:1], "", ""); env.ID != "e-0" {
		t.Fatalf("zonder live de eerste: %+v", env)
	}
}
