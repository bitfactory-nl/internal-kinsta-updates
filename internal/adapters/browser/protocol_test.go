package browser

import (
	"encoding/json"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

func TestRunRequestJSONRoundTrip(t *testing.T) {
	req := RunRequest{
		Baseline:      EnvTarget{URL: "https://prod.example.com", BasicAuth: &BasicCred{User: "bf", Pass: "s"}},
		Update:        EnvTarget{URL: "https://local.test"},
		TestAccount:   &AccountCred{User: "tester", Pass: "pw"},
		ScreenshotDir: "/tmp/run1",
		TimeoutMs:     30000,
		Flow: domain.Flow{Name: "F", Steps: []domain.Step{
			{Action: domain.StepNavigate, Target: "/"},
		}},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got RunRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Baseline.BasicAuth == nil || got.Baseline.BasicAuth.User != "bf" {
		t.Errorf("basic auth lost: %+v", got.Baseline)
	}
	if got.Flow.Steps[0].Action != domain.StepNavigate {
		t.Errorf("flow lost: %+v", got.Flow)
	}
	if got.TimeoutMs != 30000 {
		t.Errorf("timeout lost: %d", got.TimeoutMs)
	}
}

func TestRunResponseJSONRoundTrip(t *testing.T) {
	in := `{
      "steps": [
        {"index":0,"action":"navigate",
         "baseline":{"screenshot":"/t/b0.png","consoleErrors":["x"],"statusCodes":{"/":200}},
         "update":{"screenshot":"/t/u0.png","consoleErrors":[],"statusCodes":{"/":200}},
         "error":"","snapshot":""}
      ],
      "error": ""
    }`
	var resp RunResponse
	if err := json.Unmarshal([]byte(in), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Steps) != 1 {
		t.Fatalf("want 1 step, got %d", len(resp.Steps))
	}
	s := resp.Steps[0]
	if s.Baseline.Screenshot != "/t/b0.png" || s.Baseline.StatusCodes["/"] != 200 {
		t.Errorf("baseline parse: %+v", s.Baseline)
	}
	if len(s.Baseline.ConsoleErrors) != 1 || s.Baseline.ConsoleErrors[0] != "x" {
		t.Errorf("console parse: %+v", s.Baseline)
	}
}
