// Package claude calls the Anthropic Messages API for authoring flow steps,
// comparing screenshots, and self-healing selectors.
package claude

import (
	"encoding/json"

	"github.com/rdm/sites-tool/internal/domain"
)

// Current Anthropic model ids per tier. Centralised so an upgrade is one edit.
const (
	ModelHaiku  = "claude-haiku-4-5-20251001"
	ModelSonnet = "claude-sonnet-5"
	ModelOpus   = "claude-opus-4-8"
)

// tierToModel maps a logical tier (from domain.ChooseModelTier) to a model id.
func tierToModel(t domain.ModelTier) string {
	switch t {
	case domain.TierHaiku:
		return ModelHaiku
	case domain.TierOpus:
		return ModelOpus
	default:
		return ModelSonnet
	}
}

// --- Anthropic Messages API wire types (minimal subset) ---

type imageSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // "image/png"
	Data      string `json:"data"`
}

type contentBlock struct {
	Type   string       `json:"type"` // "text" | "image"
	Text   string       `json:"text,omitempty"`
	Source *imageSource `json:"source,omitempty"`
}

type message struct {
	Role    string         `json:"role"`
	Content []contentBlock `json:"content"`
}

type tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type toolChoice struct {
	Type string `json:"type"` // "tool"
	Name string `json:"name"`
}

type apiRequest struct {
	Model      string      `json:"model"`
	MaxTokens  int         `json:"max_tokens"`
	System     string      `json:"system,omitempty"`
	Messages   []message   `json:"messages"`
	Tools      []tool      `json:"tools,omitempty"`
	ToolChoice *toolChoice `json:"tool_choice,omitempty"`
}

type apiResponse struct {
	Content []struct {
		Type  string          `json:"type"`
		Input json.RawMessage `json:"input"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}
