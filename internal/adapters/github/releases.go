package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// macOSAssetSuffix is waar de app-zip in een release aan te herkennen is.
	// Op het suffix matchen en niet op de volledige naam, want die bevat de tag.
	macOSAssetSuffix = "-macOS.zip"

	// maxReleaseBodyBytes begrenst het gelezen JSON-antwoord. Release-bodies zijn
	// hooguit enkele kilobytes; een kapot antwoord mag geen onbegrensd geheugen
	// opslokken.
	maxReleaseBodyBytes = 1 << 20 // 1 MB

	// progressStep is hoe vaak de voortgangscallback tijdens een download
	// afgaat: elke 256 KB is genoeg voor een vloeiende balk zonder de
	// event-bus te overladen.
	progressStep = 256 << 10
)

// ErrNoMacOSAsset betekent dat de release bestaat maar geen macOS-zip bevat —
// bijvoorbeeld doordat de build-workflow is gefaald nadat de tag was gepusht.
var ErrNoMacOSAsset = errors.New("github: release heeft geen macOS-asset")

// ReleaseAsset is een bestand dat aan een release hangt.
type ReleaseAsset struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// Release is een GitHub-release met de asset die de macOS-app bevat.
type Release struct {
	TagName string
	Body    string
	Asset   ReleaseAsset
}

// releaseAPIResponse is de vorm waarin de REST API een release teruggeeft.
type releaseAPIResponse struct {
	TagName string         `json:"tag_name"`
	Body    string         `json:"body"`
	Assets  []ReleaseAsset `json:"assets"`
}

// ReleaseClient leest releases van één repository. Los van Client, die aan de
// plugin-repo en de contents-API gebonden is.
type ReleaseClient struct {
	token   string
	repo    string // "org/repo-name"
	baseURL string
	http    *http.Client
}

// NewReleaseClient bouwt een client voor repo (formaat "org/repo-name").
func NewReleaseClient(token, repo string) *ReleaseClient {
	return &ReleaseClient{
		token:   token,
		repo:    repo,
		baseURL: defaultBaseURL,
		// Ruim genoeg voor een zip van ~12 MB op een matige verbinding.
		http: &http.Client{Timeout: 10 * time.Minute},
	}
}

// LatestRelease haalt de nieuwste niet-draft, niet-prerelease release op en
// zoekt daarin de macOS-asset.
func (c *ReleaseClient) LatestRelease(ctx context.Context) (Release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", c.baseURL, c.repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, fmt.Errorf("release-request bouwen: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.http.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("release ophalen: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Release{}, fmt.Errorf("github api: status %d bij het ophalen van de laatste release van %s", resp.StatusCode, c.repo)
	}

	var rj releaseAPIResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxReleaseBodyBytes)).Decode(&rj); err != nil {
		return Release{}, fmt.Errorf("release parsen: %w", err)
	}

	asset, ok := pickMacOSAsset(rj.Assets)
	if !ok {
		return Release{}, fmt.Errorf("%w (tag %s)", ErrNoMacOSAsset, rj.TagName)
	}
	return Release{TagName: rj.TagName, Body: rj.Body, Asset: asset}, nil
}

// pickMacOSAsset kiest de asset die de app-bundle bevat.
func pickMacOSAsset(assets []ReleaseAsset) (ReleaseAsset, bool) {
	for _, a := range assets {
		if strings.HasSuffix(a.Name, macOSAssetSuffix) {
			return a, true
		}
	}
	return ReleaseAsset{}, false
}

// DownloadAsset streamt een release-asset naar w. onProgress mag nil zijn; is
// hij gezet, dan wordt hij elke 256 KB en één keer aan het eind aangeroepen met
// het aantal geschreven bytes en de totale omvang (0 als de server die niet
// meldt).
func (c *ReleaseClient) DownloadAsset(ctx context.Context, assetID int64, w io.Writer, onProgress func(done, total int64)) error {
	url := fmt.Sprintf("%s/repos/%s/releases/assets/%d", c.baseURL, c.repo, assetID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("download-request bouwen: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	// De API antwoordt met een redirect naar de opslaglocatie; http.Client volgt
	// die zelf.
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("asset downloaden: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("github api: status %d bij het downloaden van asset %d", resp.StatusCode, assetID)
	}

	total := resp.ContentLength
	if total < 0 {
		total = 0
	}

	var done, sindsMelding int64
	buf := make([]byte, 64<<10)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("asset wegschrijven: %w", writeErr)
			}
			done += int64(n)
			sindsMelding += int64(n)
			if onProgress != nil && sindsMelding >= progressStep {
				onProgress(done, total)
				sindsMelding = 0
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("asset lezen: %w", readErr)
		}
	}

	if onProgress != nil {
		onProgress(done, total)
	}
	return nil
}
