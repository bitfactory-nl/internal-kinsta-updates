package kinsta

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testClient(srv *httptest.Server) *Client {
	return &Client{
		apiKey:  "k",
		http:    srv.Client(),
		logHTTP: srv.Client(),
		base:    srv.URL,
	}
}

func TestGetEnvironmentLogs(t *testing.T) {
	var gotPath, gotQuery, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(`{"environment":{"container_info":{"logs":"regel1\nregel2\n"}}}`))
	}))
	defer srv.Close()

	logs, err := testClient(srv).GetEnvironmentLogs(context.Background(), "env-1", "error", 250)
	if err != nil {
		t.Fatalf("GetEnvironmentLogs: %v", err)
	}
	if logs != "regel1\nregel2\n" {
		t.Errorf("logs = %q", logs)
	}
	if gotPath != "/sites/environments/env-1/logs" {
		t.Errorf("path = %q", gotPath)
	}
	if !strings.Contains(gotQuery, "file_name=error") || !strings.Contains(gotQuery, "lines=250") {
		t.Errorf("query = %q", gotQuery)
	}
	if gotAuth != "Bearer k" {
		t.Errorf("auth = %q", gotAuth)
	}
}

func TestGetEnvironmentLogsClampsLines(t *testing.T) {
	tests := []struct {
		naam string
		in   int
		want string
	}{
		{"nul valt terug op de API-default", 0, "lines=1000"},
		{"negatief valt terug op de API-default", -5, "lines=1000"},
		{"boven het maximum wordt afgekapt", 999999, "lines=20000"},
		{"binnen bereik blijft staan", 4321, "lines=4321"},
	}
	for _, tt := range tests {
		t.Run(tt.naam, func(t *testing.T) {
			var gotQuery string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotQuery = r.URL.RawQuery
				w.Write([]byte(`{"environment":{"container_info":{"logs":""}}}`))
			}))
			defer srv.Close()

			if _, err := testClient(srv).GetEnvironmentLogs(context.Background(), "e", "error", tt.in); err != nil {
				t.Fatalf("GetEnvironmentLogs: %v", err)
			}
			if !strings.Contains(gotQuery, tt.want) {
				t.Errorf("query = %q, wil %q", gotQuery, tt.want)
			}
		})
	}
}

func TestGetEnvironmentLogsWeigertOnbekendBestand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("er had geen request gedaan mogen worden")
	}))
	defer srv.Close()

	for _, naam := range []string{"", "debug", "error.log", "../../etc/passwd"} {
		if _, err := testClient(srv).GetEnvironmentLogs(context.Background(), "e", naam, 10); err == nil {
			t.Errorf("bestandsnaam %q werd geaccepteerd", naam)
		}
	}
}

func TestGetEnvironmentLogsWeigertLeegEnvID(t *testing.T) {
	c := &Client{apiKey: "k"}
	if _, err := c.GetEnvironmentLogs(context.Background(), "", "error", 10); err == nil {
		t.Fatal("leeg environment-id werd geaccepteerd")
	}
}

func TestGetEnvironmentLogsGeeftAPIFoutDoor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if _, err := testClient(srv).GetEnvironmentLogs(context.Background(), "e", "error", 10); err == nil {
		t.Fatal("status 404 leverde geen fout")
	}
}

func TestGetEnvironmentLogsRespecteertContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := testClient(srv).GetEnvironmentLogs(ctx, "e", "error", 10); err == nil {
		t.Fatal("verwachtte een fout bij een verlopen context")
	}
}
