package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rdm/sites-tool/internal/domain"
)

const healSystem = `Een teststap kon een element niet vinden. Op basis van de accessibility-tree ` +
	`(YAML) en de beschrijving van het gezochte element geef je één robuuste CSS-selector terug ` +
	`die dat element aanwijst. Geef uitsluitend het gereedschap terug.`

var healTool = tool{
	Name:        "emit_selector",
	Description: "Geef de CSS-selector terug.",
	InputSchema: json.RawMessage(`{"type":"object","properties":{"selector":{"type":"string"}},"required":["selector"]}`),
}

// Heal returns a CSS selector for target, derived from the page accessibility
// snapshot (YAML from Playwright's ariaSnapshot).
func (c *Client) Heal(ctx context.Context, ariaSnapshot, target string) (string, error) {
	tier := domain.ChooseModelTier(domain.RoutingInput{Task: domain.TaskHeal})
	in, err := c.toolCall(ctx, tier, healSystem,
		[]contentBlock{{Type: "text", Text: "Gezocht element: " + target + "\n\nAccessibility-tree:\n" + ariaSnapshot}},
		healTool)
	if err != nil {
		return "", err
	}
	var out struct {
		Selector string `json:"selector"`
	}
	if err := json.Unmarshal(in, &out); err != nil {
		return "", fmt.Errorf("parse selector: %w", err)
	}
	if strings.TrimSpace(out.Selector) == "" {
		return "", fmt.Errorf("lege selector van Claude")
	}
	return out.Selector, nil
}
