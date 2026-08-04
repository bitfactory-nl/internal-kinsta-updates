package services

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tarEntry beschrijft één ingang voor maakTarGz.
type tarEntry struct {
	naam     string
	inhoud   string
	typeflag byte
	linkname string
	mode     int64
}

func maakTarGz(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		tf := e.typeflag
		if tf == 0 {
			tf = tar.TypeReg
		}
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		hdr := &tar.Header{
			Name:     e.naam,
			Mode:     mode,
			Size:     int64(len(e.inhoud)),
			Typeflag: tf,
			Linkname: e.linkname,
		}
		if tf == tar.TypeDir {
			hdr.Size = 0
			hdr.Mode = 0o755
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if tf == tar.TypeReg {
			if _, err := tw.Write([]byte(e.inhoud)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPakUitOnderSchrijftBestanden(t *testing.T) {
	doel := t.TempDir()
	archief := maakTarGz(t, []tarEntry{
		{naam: "2026/", typeflag: tar.TypeDir},
		{naam: "2026/08/", typeflag: tar.TypeDir},
		{naam: "2026/08/foto.jpg", inhoud: "jpeg-bytes"},
		{naam: "2026/08/foto-150x150.jpg", inhoud: "thumb"},
	})

	res, err := pakUitOnder(bytes.NewReader(archief), doel, nil)
	if err != nil {
		t.Fatalf("pakUitOnder: %v", err)
	}
	if res.Files != 2 {
		t.Errorf("Files = %d, wil 2", res.Files)
	}
	if res.Bytes != int64(len("jpeg-bytes")+len("thumb")) {
		t.Errorf("Bytes = %d", res.Bytes)
	}
	data, err := os.ReadFile(filepath.Join(doel, "2026", "08", "foto.jpg"))
	if err != nil {
		t.Fatalf("uitgepakt bestand lezen: %v", err)
	}
	if string(data) != "jpeg-bytes" {
		t.Errorf("inhoud = %q", data)
	}
}

func TestPakUitOnderOverschrijftBestaand(t *testing.T) {
	doel := t.TempDir()
	if err := os.MkdirAll(filepath.Join(doel, "2026"), 0o755); err != nil {
		t.Fatal(err)
	}
	bestaand := filepath.Join(doel, "2026", "foto.jpg")
	if err := os.WriteFile(bestaand, []byte("oude-versie-veel-langer"), 0o644); err != nil {
		t.Fatal(err)
	}

	archief := maakTarGz(t, []tarEntry{{naam: "2026/foto.jpg", inhoud: "nieuw"}})
	if _, err := pakUitOnder(bytes.NewReader(archief), doel, nil); err != nil {
		t.Fatalf("pakUitOnder: %v", err)
	}

	data, _ := os.ReadFile(bestaand)
	if string(data) != "nieuw" {
		t.Errorf("inhoud = %q, wil %q — bestaande bestanden horen overschreven te worden (afgesproken gedrag)", data, "nieuw")
	}
}

// TestPakUitOnderWeigertPadOntsnapping is de belangrijkste test van dit
// bestand: een archief is onvertrouwde invoer en een ingang die buiten de
// doelmap wil schrijven mag nooit uitgepakt worden.
func TestPakUitOnderWeigertPadOntsnapping(t *testing.T) {
	ouder := t.TempDir()
	doel := filepath.Join(ouder, "uploads")
	if err := os.MkdirAll(doel, 0o755); err != nil {
		t.Fatal(err)
	}

	archief := maakTarGz(t, []tarEntry{
		{naam: "../ontsnapt.txt", inhoud: "kwaad"},
		{naam: "../../ver/weg.txt", inhoud: "kwaad"},
		{naam: "/absoluut/pad.txt", inhoud: "kwaad"},
		{naam: "2026/goed.jpg", inhoud: "ok"},
	})

	res, err := pakUitOnder(bytes.NewReader(archief), doel, nil)
	if err != nil {
		t.Fatalf("pakUitOnder: %v", err)
	}
	if res.Files != 1 {
		t.Errorf("Files = %d, wil 1 — alleen het veilige bestand mag geschreven zijn", res.Files)
	}
	if len(res.Skipped) != 3 {
		t.Errorf("Skipped = %v, wil 3 geweigerde ingangen", res.Skipped)
	}
	if _, err := os.Stat(filepath.Join(ouder, "ontsnapt.txt")); err == nil {
		t.Fatal("een ../-ingang is buiten de doelmap geschreven")
	}
	if _, err := os.Stat(filepath.Join(doel, "2026", "goed.jpg")); err != nil {
		t.Errorf("het veilige bestand had geschreven moeten worden: %v", err)
	}
}

func TestPakUitOnderSlaatLinksOver(t *testing.T) {
	doel := t.TempDir()
	archief := maakTarGz(t, []tarEntry{
		{naam: "evil-link", typeflag: tar.TypeSymlink, linkname: "/etc/passwd"},
		{naam: "hard-link", typeflag: tar.TypeLink, linkname: "2026/foto.jpg"},
		{naam: "2026/foto.jpg", inhoud: "ok"},
	})

	res, err := pakUitOnder(bytes.NewReader(archief), doel, nil)
	if err != nil {
		t.Fatalf("pakUitOnder: %v", err)
	}
	if res.Files != 1 {
		t.Errorf("Files = %d, wil 1", res.Files)
	}
	if len(res.Skipped) != 2 {
		t.Errorf("Skipped = %v, wil beide links overgeslagen", res.Skipped)
	}
	if _, err := os.Lstat(filepath.Join(doel, "evil-link")); err == nil {
		t.Error("een symlink is uitgepakt; die kan na het uitpakken buiten de map wijzen")
	}
}

func TestPakUitOnderMeldtOngeldigArchief(t *testing.T) {
	doel := t.TempDir()
	_, err := pakUitOnder(strings.NewReader("dit is geen gzip"), doel, nil)
	if err == nil {
		t.Fatal("verwachtte een fout op een ongeldig archief")
	}
}

func TestVeiligDoelpad(t *testing.T) {
	wortel := filepath.Join(string(filepath.Separator), "tmp", "uploads")
	prefix := wortel + string(filepath.Separator)

	geldig := []string{"2026/foto.jpg", "./2026/foto.jpg", "sites/2/x.png", "a/../b.jpg"}
	for _, naam := range geldig {
		if _, ok := veiligDoelpad(wortel, prefix, naam); !ok {
			t.Errorf("veiligDoelpad(%q) geweigerd, wil toegestaan", naam)
		}
	}

	ongeldig := []string{"../buiten.jpg", "../../ver/weg.jpg", "/etc/passwd", "2026/../../buiten.jpg"}
	for _, naam := range ongeldig {
		if _, ok := veiligDoelpad(wortel, prefix, naam); ok {
			t.Errorf("veiligDoelpad(%q) toegestaan, wil geweigerd", naam)
		}
	}
}
