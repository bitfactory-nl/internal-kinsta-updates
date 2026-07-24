package config

import (
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

func TestResolveTestAccess(t *testing.T) {
	// Plain-string secrets resolve as-is (ResolveSecret only hits keychain
	// for "keychain:" refs), so no macOS keychain is needed in tests.
	cfg := &domain.TestingCfg{
		BasicAuth:   map[string]domain.BasicAuth{"acc": {User: "bf", Pass: "s3cret"}},
		TestAccount: &domain.TestAccount{User: "tester", Pass: "pw"},
	}

	acc, err := ResolveTestAccess(cfg, domain.EnvAcc)
	if err != nil {
		t.Fatalf("acc: %v", err)
	}
	if acc.BasicAuthUser != "bf" || acc.BasicAuthPass != "s3cret" {
		t.Errorf("basic auth = %+v", acc)
	}
	if acc.TestUser != "tester" || acc.TestPass != "pw" {
		t.Errorf("test account = %+v", acc)
	}

	// prod has no basic-auth entry -> empty basic-auth, test account still set.
	prod, err := ResolveTestAccess(cfg, domain.EnvProd)
	if err != nil {
		t.Fatalf("prod: %v", err)
	}
	if prod.BasicAuthUser != "" || prod.BasicAuthPass != "" {
		t.Errorf("expected no basic auth for prod, got %+v", prod)
	}
}

func TestResolveTestAccessNil(t *testing.T) {
	got, err := ResolveTestAccess(nil, domain.EnvLocal)
	if err != nil {
		t.Fatalf("nil cfg: %v", err)
	}
	if (got != ResolvedAccess{}) {
		t.Errorf("expected zero ResolvedAccess, got %+v", got)
	}
}
