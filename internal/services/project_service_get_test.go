package services

import (
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

func TestProjectServiceGet(t *testing.T) {
	s := NewProjectService(nil)
	s.projects = []domain.Project{{ID: "abc", DisplayName: "X"}}
	got, ok := s.Get("abc")
	if !ok || got.DisplayName != "X" {
		t.Fatalf("Get(abc) = %+v, %v", got, ok)
	}
	if _, ok := s.Get("nope"); ok {
		t.Error("Get(nope) should be false")
	}
}
