package config

import (
	"fmt"

	"github.com/rdm/sites-tool/internal/domain"
)

// ResolvedAccess holds runtime credentials for one environment, with secrets
// already resolved from the keychain.
type ResolvedAccess struct {
	BasicAuthUser string
	BasicAuthPass string
	TestUser      string
	TestPass      string
}

// ResolveTestAccess resolves basic-auth and test-account secrets for env.
// A nil cfg or a missing basic-auth entry yields empty fields (not an error).
func ResolveTestAccess(cfg *domain.TestingCfg, env domain.EnvKey) (ResolvedAccess, error) {
	var out ResolvedAccess
	if cfg == nil {
		return out, nil
	}
	if ba, ok := cfg.BasicAuth[string(env)]; ok {
		pass, err := ResolveSecret(ba.Pass)
		if err != nil {
			return out, fmt.Errorf("basic-auth %s: %w", env, err)
		}
		out.BasicAuthUser = ba.User
		out.BasicAuthPass = pass
	}
	if cfg.TestAccount != nil {
		pass, err := ResolveSecret(cfg.TestAccount.Pass)
		if err != nil {
			return out, fmt.Errorf("test-account: %w", err)
		}
		out.TestUser = cfg.TestAccount.User
		out.TestPass = pass
	}
	return out, nil
}
