package domain

import "testing"

func TestStepTypeValid(t *testing.T) {
	cases := []struct {
		in   StepType
		want bool
	}{
		{StepNavigate, true},
		{StepClick, true},
		{StepInput, true},
		{StepLogin, true},
		{StepWait, true},
		{StepAssert, true},
		{StepType("bogus"), false},
		{StepType(""), false},
	}
	for _, c := range cases {
		if got := c.in.Valid(); got != c.want {
			t.Errorf("StepType(%q).Valid() = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestEnvKeyValid(t *testing.T) {
	cases := []struct {
		in   EnvKey
		want bool
	}{
		{EnvLocal, true},
		{EnvAcc, true},
		{EnvProd, true},
		{EnvKey("staging"), false},
	}
	for _, c := range cases {
		if got := c.in.Valid(); got != c.want {
			t.Errorf("EnvKey(%q).Valid() = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestProjectConfigHasTesting(t *testing.T) {
	cfg := ProjectConfig{
		Testing: &TestingCfg{
			Environments: map[string]string{"local": "https://x.test"},
			BasicAuth:    map[string]BasicAuth{"acc": {User: "u", Pass: "keychain:p"}},
			TestAccount:  &TestAccount{User: "t", Pass: "keychain:q"},
		},
	}
	if cfg.Testing.Environments["local"] != "https://x.test" {
		t.Fatal("local env not stored")
	}
	if cfg.Testing.BasicAuth["acc"].User != "u" {
		t.Fatal("basic auth not stored")
	}
	if cfg.Testing.TestAccount.User != "t" {
		t.Fatal("test account not stored")
	}
}
