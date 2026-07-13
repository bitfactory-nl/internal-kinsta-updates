package domain

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

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

func TestTestingCfgYAMLRoundTrip(t *testing.T) {
	orig := ProjectConfig{
		Testing: &TestingCfg{
			Environments: map[string]string{"local": "https://x.test"},
			BasicAuth:    map[string]BasicAuth{"acc": {User: "u", Pass: "keychain:p"}},
			TestAccount:  &TestAccount{User: "t", Pass: "keychain:q"},
		},
	}

	out, err := yaml.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Snake_case keys must appear so a tag typo fails the test.
	for _, key := range []string{"testing:", "environments:", "basic_auth:", "test_account:"} {
		if !strings.Contains(string(out), key) {
			t.Errorf("marshalled YAML missing key %q\n---\n%s", key, out)
		}
	}

	var got ProjectConfig
	if err := yaml.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Testing == nil {
		t.Fatal("testing not round-tripped")
	}
	if got.Testing.Environments["local"] != "https://x.test" {
		t.Errorf("local env = %q, want %q", got.Testing.Environments["local"], "https://x.test")
	}
	if got.Testing.BasicAuth["acc"].User != "u" || got.Testing.BasicAuth["acc"].Pass != "keychain:p" {
		t.Errorf("basic auth = %+v, want {u keychain:p}", got.Testing.BasicAuth["acc"])
	}
	if got.Testing.TestAccount == nil || got.Testing.TestAccount.User != "t" || got.Testing.TestAccount.Pass != "keychain:q" {
		t.Errorf("test account = %+v, want {t keychain:q}", got.Testing.TestAccount)
	}
}

func TestResolveEnvURL(t *testing.T) {
	p := Project{
		Config: ProjectConfig{Testing: &TestingCfg{
			Environments: map[string]string{"local": "https://cefetra.test"},
		}},
		Deploy: DeployConf{Link: DeployLinks{Acc: "https://acc.cefetra.com", Prod: "https://cefetra.com"}},
	}

	cases := []struct {
		env     EnvKey
		want    string
		wantErr bool
	}{
		{EnvLocal, "https://cefetra.test", false},
		{EnvAcc, "https://acc.cefetra.com", false},
		{EnvProd, "https://cefetra.com", false},
		{EnvKey("staging"), "", true},
	}
	for _, c := range cases {
		got, err := ResolveEnvURL(p, c.env)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", c.env, err, c.wantErr)
		}
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.env, got, c.want)
		}
	}
}

func TestResolveEnvURLMissingLocal(t *testing.T) {
	p := Project{}
	if _, err := ResolveEnvURL(p, EnvLocal); err == nil {
		t.Error("expected error for missing local URL")
	}
}

func TestDiffRegressions(t *testing.T) {
	baseline := PageObservation{
		ConsoleErrors: []string{"shared error"},
		StatusCodes:   map[string]int{"/ok": 200, "/always404": 404, "/always500": 500},
	}
	update := PageObservation{
		ConsoleErrors: []string{"shared error", "NEW error"},
		StatusCodes: map[string]int{
			"/ok":        200,
			"/always404": 404, // shared 4xx -> not a regression
			"/always500": 500, // 5xx -> always reported (hard)
			"/new404":    404, // new 4xx -> regression, not hard
			"/new500":    500, // new 5xx -> regression, hard
		},
	}

	got := DiffRegressions(baseline, update)

	want := []Regression{
		{Kind: RegConsole, Detail: "NEW error", Hard: false},
		{Kind: RegStatus, Detail: "/always500 → 500", Hard: true},
		{Kind: RegStatus, Detail: "/new404 → 404", Hard: false},
		{Kind: RegStatus, Detail: "/new500 → 500", Hard: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DiffRegressions()\n got  %+v\n want %+v", got, want)
	}
}

func TestDiffRegressionsEmpty(t *testing.T) {
	if got := DiffRegressions(PageObservation{}, PageObservation{}); len(got) != 0 {
		t.Errorf("expected no regressions, got %+v", got)
	}
}
