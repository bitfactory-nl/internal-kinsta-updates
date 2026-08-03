package browser

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// nepNode legt een uitvoerbaar bestand neer dat als node moet doorgaan.
func nepNode(t *testing.T, pad string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(pad), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pad, []byte("#!/bin/sh\necho v20.0.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return pad
}

// resetNodeZoeker maakt de cache leeg en herstelt de seams na de test.
func resetNodeZoeker(t *testing.T) {
	t.Helper()
	origLook, origPaden := nodeLookPath, nodeVastePaden
	t.Cleanup(func() {
		nodeLookPath, nodeVastePaden = origLook, origPaden
	})
	// De publieke NodeBin() cachet; de tests gebruiken daarom zoekNode() direct.
	nodeLookPath = func(string) (string, error) { return "", errors.New("niet op PATH") }
}

func TestZoekNodeOmgevingsvariabeleWint(t *testing.T) {
	resetNodeZoeker(t)
	t.Setenv("RDM_NODE", "/eigen/node")
	if got := zoekNode(); got != "/eigen/node" {
		t.Errorf("zoekNode = %q, wil de override", got)
	}
}

func TestZoekNodeViaVastPad(t *testing.T) {
	resetNodeZoeker(t)
	t.Setenv("RDM_NODE", "")
	pad := nepNode(t, filepath.Join(t.TempDir(), "node"))
	nodeVastePaden = []string{"/bestaat/niet/node", pad}

	if got := zoekNode(); got != pad {
		t.Errorf("zoekNode = %q, wil %q — dit is het geval van een .app zonder shell-PATH", got, pad)
	}
}

func TestZoekNodeSlaatNietUitvoerbaarBestandOver(t *testing.T) {
	resetNodeZoeker(t)
	t.Setenv("RDM_NODE", "")
	dir := t.TempDir()
	nietUitvoerbaar := filepath.Join(dir, "node")
	if err := os.WriteFile(nietUitvoerbaar, []byte("geen programma"), 0o644); err != nil {
		t.Fatal(err)
	}
	goed := nepNode(t, filepath.Join(t.TempDir(), "node"))
	nodeVastePaden = []string{nietUitvoerbaar, goed}

	if got := zoekNode(); got != goed {
		t.Errorf("zoekNode = %q; een bestand zonder x-bit is geen node", got)
	}
}

func TestVersieLagerVergelijktGetallen(t *testing.T) {
	// Tekstvergelijking zou 9.0.0 boven 22.3.0 zetten; dat mag niet, want dan pakt de
	// tool een Node die de sidecar niet aankan.
	if !versieLager("9.0.0", "22.3.0") {
		t.Error("9.0.0 hoort lager te zijn dan 22.3.0")
	}
	if versieLager("20.11.0", "20.9.0") {
		t.Error("20.11.0 hoort hoger te zijn dan 20.9.0")
	}
}

func TestVersieUitPad(t *testing.T) {
	got := versieUitPad("/Users/x/.nvm/versions/node/v20.11.0/bin/node")
	if got != "20.11.0" {
		t.Errorf("versieUitPad = %q, wil 20.11.0", got)
	}
}
