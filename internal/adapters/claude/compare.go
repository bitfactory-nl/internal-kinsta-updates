package claude

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/rdm/sites-tool/internal/domain"
)

const compareSystem = `Je vergelijkt twee screenshots van dezelfde pagina op twee omgevingen ` +
	`(release vs update). De omgevingen hebben ANDERE inhoud/data (andere teksten, afbeeldingen, ` +
	`aantallen) — dat is normaal en GEEN fout. Rapporteer alleen echte afwijkingen en ` +
	`categoriseer elk verschil:
- layout-break: kapotte/overlappende layout, verschoven of afgebroken secties
- missing-element: een element/sectie ontbreekt op de update-kant
- styling: kleur/font/spacing-afwijking
- content-only: puur inhoud/data-verschil (bijna altijd ernst 'laag', negeerbaar)
Ernst 'hoog' voor layout-break/missing-element die de pagina breken, anders 'laag'. ` +
	`Geef uitsluitend het gereedschap terug.`

var compareTool = tool{
	Name:        "emit_findings",
	Description: "Geef de gecategoriseerde verschillen terug.",
	InputSchema: json.RawMessage(`{
		"type":"object",
		"properties":{"findings":{"type":"array","items":{"type":"object",
			"properties":{
				"category":{"type":"string","enum":["layout-break","missing-element","styling","content-only"]},
				"severity":{"type":"string","enum":["hoog","laag"]},
				"where":{"type":"string"},
				"description":{"type":"string"}
			},"required":["category","severity","description"]}}},
		"required":["findings"]}`),
}

// Compare returns categorised visual differences between two PNG screenshots.
// stepDesc gives Claude context; highImpact escalates the model tier to opus.
func (c *Client) Compare(ctx context.Context, baselinePNG, updatePNG []byte, stepDesc string, highImpact bool) ([]domain.Finding, error) {
	tier := domain.ChooseModelTier(domain.RoutingInput{Task: domain.TaskCompare, HighImpact: highImpact, Override: c.Override})
	content := []contentBlock{
		{Type: "text", Text: "Context: " + stepDesc + "\nEerste afbeelding = release (baseline), tweede = update."},
		{Type: "image", Source: &imageSource{Type: "base64", MediaType: "image/png", Data: base64.StdEncoding.EncodeToString(baselinePNG)}},
		{Type: "image", Source: &imageSource{Type: "base64", MediaType: "image/png", Data: base64.StdEncoding.EncodeToString(updatePNG)}},
	}
	in, err := c.toolCall(ctx, tier, compareSystem, content, compareTool)
	if err != nil {
		return nil, err
	}
	var out struct {
		Findings []domain.Finding `json:"findings"`
	}
	if err := json.Unmarshal(in, &out); err != nil {
		return nil, fmt.Errorf("parse findings: %w", err)
	}
	return out.Findings, nil
}
