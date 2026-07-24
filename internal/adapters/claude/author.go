package claude

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rdm/sites-tool/internal/domain"
)

const authorSystem = `Je zet een testscenario in natuurlijke taal om in concrete stappen voor ` +
	`een browsertest van een WordPress-site. Gebruik alleen de acties: navigate, click, type, ` +
	`login, wait, assert. Voor click/type/assert beschrijf je 'target' als de zichtbare ` +
	`tekst of het label. Voor 'type' vul je ook 'value'. Laat 'selector' leeg. ` +
	`Geef uitsluitend het gereedschap terug.`

var authorTool = tool{
	Name:        "emit_steps",
	Description: "Geef de flow-stappen terug.",
	InputSchema: json.RawMessage(`{
		"type":"object",
		"properties":{"steps":{"type":"array","items":{"type":"object",
			"properties":{
				"action":{"type":"string","enum":["navigate","click","type","login","wait","assert"]},
				"target":{"type":"string"},
				"value":{"type":"string"},
				"selector":{"type":"string"}
			},"required":["action"]}}},
		"required":["steps"]}`),
}

// Author converts a natural-language scenario into validated flow steps.
func (c *Client) Author(ctx context.Context, description string) ([]domain.Step, error) {
	tier := domain.ChooseModelTier(domain.RoutingInput{Task: domain.TaskAuthor, Override: c.Override})
	in, err := c.toolCall(ctx, tier, authorSystem,
		[]contentBlock{{Type: "text", Text: description}}, authorTool)
	if err != nil {
		return nil, err
	}
	var out struct {
		Steps []domain.Step `json:"steps"`
	}
	if err := json.Unmarshal(in, &out); err != nil {
		return nil, fmt.Errorf("parse steps: %w", err)
	}
	for i, s := range out.Steps {
		if !s.Action.Valid() {
			return nil, fmt.Errorf("stap %d: ongeldige actie %q", i, s.Action)
		}
	}
	return out.Steps, nil
}
