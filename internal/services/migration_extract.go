package services

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// extractResultaat is what one extracted tar stream produced.
type extractResultaat struct {
	Files   int
	Bytes   int64
	Skipped []string // entries that were deliberately not written, with the reason
}

// pakUitOnder extracts a gzipped tar stream into destRoot, overwriting existing
// files.
//
// Every entry's path is validated before anything is written: an archive is
// untrusted input, and a crafted entry name ("../../etc/…", an absolute path,
// or a symlink pointing outside) is the classic way to make an extractor write
// where it shouldn't. Anything that does not resolve to a path strictly inside
// destRoot is skipped and reported rather than written. Symlinks and hardlinks
// are skipped entirely: WordPress uploads have no legitimate use for them, and
// a link is the one entry type whose target can escape after extraction.
func pakUitOnder(r io.Reader, destRoot string, onFile func(pad string, geschreven int64)) (extractResultaat, error) {
	var res extractResultaat

	wortel, err := filepath.Abs(destRoot)
	if err != nil {
		return res, err
	}
	if err := os.MkdirAll(wortel, 0o755); err != nil {
		return res, err
	}
	prefix := wortel + string(filepath.Separator)

	gz, err := gzip.NewReader(r)
	if err != nil {
		return res, fmt.Errorf("archief uitpakken: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return res, nil
		}
		if err != nil {
			return res, fmt.Errorf("archief lezen: %w", err)
		}

		switch hdr.Typeflag {
		case tar.TypeSymlink, tar.TypeLink:
			res.Skipped = append(res.Skipped, hdr.Name+" (link overgeslagen)")
			continue
		case tar.TypeDir, tar.TypeReg:
			// Deze twee pakken we uit; al het andere (fifo, device, char) hoort
			// niet in een uploads-map en slaan we over.
		default:
			res.Skipped = append(res.Skipped, hdr.Name+" (bestandstype overgeslagen)")
			continue
		}

		doel, ok := veiligDoelpad(wortel, prefix, hdr.Name)
		if !ok {
			res.Skipped = append(res.Skipped, hdr.Name+" (pad buiten de doelmap)")
			continue
		}

		if hdr.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(doel, 0o755); err != nil {
				return res, err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(doel), 0o755); err != nil {
			return res, err
		}
		n, err := schrijfBestand(doel, tr, hdr.FileInfo().Mode().Perm())
		if err != nil {
			return res, err
		}
		res.Files++
		res.Bytes += n
		if onFile != nil {
			onFile(doel, res.Bytes)
		}
	}
}

// veiligDoelpad maps an archive entry name onto a path inside wortel, or
// reports that it cannot be done safely.
func veiligDoelpad(wortel, prefix, naam string) (string, bool) {
	schoon := filepath.Clean(filepath.FromSlash(naam))
	if filepath.IsAbs(schoon) || strings.HasPrefix(schoon, ".."+string(filepath.Separator)) || schoon == ".." {
		return "", false
	}
	doel := filepath.Join(wortel, schoon)
	// Join heeft al genormaliseerd; deze check vangt wat er alsnog buiten valt.
	if doel != wortel && !strings.HasPrefix(doel, prefix) {
		return "", false
	}
	return doel, true
}

// schrijfBestand writes one file, overwriting whatever is there. The user chose
// "always overwrite" so the local copy is guaranteed to match production, at
// the cost of re-downloading unchanged files.
func schrijfBestand(pad string, r io.Reader, mode os.FileMode) (int64, error) {
	if mode == 0 {
		mode = 0o644
	}
	f, err := os.OpenFile(pad, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(f, r)
	closeErr := f.Close()
	if err != nil {
		return n, err
	}
	return n, closeErr
}
