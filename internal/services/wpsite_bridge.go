package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/rdm/sites-tool/internal/domain"
)

const (
	bridgeReadTimeout    = 15 * time.Second
	bridgeRefreshTimeout = 60 * time.Second
)

// bridgeRunTimeout is een var zodat tests een korte wachttijd kunnen zetten.
var bridgeRunTimeout = 5 * time.Minute

// errBridgeTimeout: de site gaf geen antwoord binnen de wachttijd. Bij een
// uitvoering betekent dat níet dat de update mislukt is.
var errBridgeTimeout = errors.New("geen antwoord binnen de wachttijd — de update kan wel gelukt zijn; ververs en controleer")

// isTimeout herkent een verlopen wachttijd in een transportfout.
func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// siteMetSecret doet de gedeelde precheck voor elke bridge-aanroep: site
// bestaat, wachtwoord staat in de Keychain, bridge is actief.
func (s *WPSiteService) siteMetSecret(siteID string) (domain.WPSite, string, error) {
	site, err := s.find(siteID)
	if err != nil {
		return domain.WPSite{}, "", err
	}
	pw, err := s.secret(siteID)
	if err != nil || pw == "" {
		return domain.WPSite{}, "", errors.New("geen Application Password opgeslagen")
	}
	if !site.Bridge {
		return domain.WPSite{}, "", errors.New("nb-mcp-bridge is niet actief op deze site — test eerst de verbinding")
	}
	return site, pw, nil
}

// bridgeCall doet één verzoek naar de nb-mcp-bridge en decodeert het antwoord
// in out. Foutmeldingen noemen nooit het wachtwoord en nooit de transportfout
// zelf (die kan de URL met details bevatten).
func (s *WPSiteService) bridgeCall(site domain.WPSite, pw, method, path string, body any, timeout time.Duration, out any) error {
	var rdr io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(data)
	}
	url := strings.TrimRight(site.URL, "/") + "/wp-json/nb-mcp/v1" + path
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		return errors.New("site niet bereikbaar")
	}
	req.SetBasicAuth(site.Username, pw)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.longClient(timeout).Do(req)
	if err != nil {
		if isTimeout(err) {
			return errBridgeTimeout
		}
		return errors.New("site niet bereikbaar")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out == nil {
			return nil
		}
		if err := json.Unmarshal(raw, out); err != nil {
			return errors.New("onverwacht antwoord van de nb-mcp-bridge")
		}
		return nil
	}
	return bridgeFout(resp.StatusCode, raw)
}

// bridgeFout vertaalt een HTTP-fout van WordPress naar een leesbare melding.
func bridgeFout(status int, raw []byte) error {
	switch status {
	case http.StatusUnauthorized:
		return errors.New("gebruiker of Application Password klopt niet")
	case http.StatusNotFound:
		return errors.New("nb-mcp-bridge ontbreekt op deze site")
	case http.StatusForbidden, http.StatusConflict:
		var wpErr struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(raw, &wpErr) == nil && strings.TrimSpace(wpErr.Message) != "" {
			msg := strings.TrimSpace(wpErr.Message)
			if len(msg) > 300 {
				msg = msg[:300]
			}
			return errors.New(msg)
		}
		return fmt.Errorf("WordPress weigerde het verzoek (HTTP %d)", status)
	default:
		return fmt.Errorf("site niet bereikbaar (HTTP %d)", status)
	}
}
