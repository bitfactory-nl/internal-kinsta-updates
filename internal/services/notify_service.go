package services

import (
	"fmt"
	"os/exec"
)

type NotifyService struct{}

func NewNotifyService() *NotifyService { return &NotifyService{} }

// Send sends a macOS notification via osascript.
func (s *NotifyService) Send(title, message string) error {
	script := fmt.Sprintf(
		`display notification %q with title %q`,
		message, title,
	)
	cmd := exec.Command("osascript", "-e", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("notify: %w (%s)", err, out)
	}
	return nil
}
