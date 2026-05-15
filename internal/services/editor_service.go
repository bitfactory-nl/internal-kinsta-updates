package services

import (
	"fmt"
	"os/exec"

	"github.com/rdm/sites-tool/internal/config"
)

type EditorService struct {
	cfg *config.Global
}

func NewEditorService(cfg *config.Global) *EditorService {
	return &EditorService{cfg: cfg}
}

// OpenInEditor opens the project path in the configured editor.
func (s *EditorService) OpenInEditor(projectID string, projectPath string) error {
	editor := s.cfg.Editor
	var cmd *exec.Cmd
	switch editor {
	case "vscode":
		cmd = exec.Command("code", projectPath)
	case "phpstorm":
		cmd = exec.Command("phpstorm", projectPath)
	case "cursor":
		cmd = exec.Command("cursor", projectPath)
	default:
		cmd = exec.Command("cursor", projectPath)
	}
	if err := cmd.Start(); err != nil {
		// Try open as fallback
		fallback := exec.Command("open", "-a", editorAppName(editor), projectPath)
		if err2 := fallback.Start(); err2 != nil {
			return fmt.Errorf("open in editor (%s): %w", editor, err)
		}
	}
	return nil
}

// GetEditor returns the currently configured editor slug.
func (s *EditorService) GetEditor() string {
	return s.cfg.Editor
}

// SetEditor updates the configured editor and saves config.
func (s *EditorService) SetEditor(editor string) error {
	s.cfg.Editor = editor
	return config.SaveGlobal(*s.cfg)
}

func editorAppName(slug string) string {
	switch slug {
	case "vscode":
		return "Visual Studio Code"
	case "phpstorm":
		return "PhpStorm"
	default:
		return "Cursor"
	}
}
