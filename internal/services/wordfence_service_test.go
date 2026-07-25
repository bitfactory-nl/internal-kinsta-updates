package services

import (
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

func TestIsVersionAffected(t *testing.T) {
	sw := domain.AffectedSoftware{
		Type:          "plugin",
		Slug:          "cf7",
		AffectedFrom:  "*",
		FromInclusive: true,
		AffectedTo:    "5.3.1",
		ToInclusive:   true,
	}
	if !isVersionAffected("5.3.1", sw) {
		t.Error("5.3.1 should be affected (inclusive upper bound)")
	}
	if isVersionAffected("5.3.2", sw) {
		t.Error("5.3.2 should NOT be affected")
	}
	if !isVersionAffected("1.0", sw) {
		t.Error("1.0 should be affected (from *)")
	}

	sw2 := domain.AffectedSoftware{
		Type: "plugin", Slug: "x",
		AffectedFrom: "2.0", FromInclusive: false,
		AffectedTo: "2.5", ToInclusive: false,
	}
	if isVersionAffected("2.0", sw2) {
		t.Error("2.0 excluded by from_inclusive=false")
	}
	if !isVersionAffected("2.3", sw2) {
		t.Error("2.3 in open range")
	}
	if isVersionAffected("2.5", sw2) {
		t.Error("2.5 excluded by to_inclusive=false")
	}
}
