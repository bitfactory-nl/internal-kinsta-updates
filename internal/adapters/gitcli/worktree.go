package gitcli

import (
	"context"
	"fmt"
	"os"
)

// WorktreeAdd voegt op worktreePath een nieuwe worktree toe op een verse
// branch (branch) vanaf fromRef. Zo kan een core-update geïsoleerd gebouwd
// worden zonder de bestaande checkout van de gebruiker aan te raken.
func WorktreeAdd(ctx context.Context, repoDir, worktreePath, branch, fromRef string) error {
	_, err := Run(ctx, repoDir, "worktree", "add", "-b", branch, worktreePath, fromRef)
	if err != nil {
		return fmt.Errorf("git worktree add %s %s: %w", branch, worktreePath, err)
	}
	return nil
}

// WorktreeRemove verwijdert de worktree op worktreePath. Dit is best-effort
// opruimen: als --force faalt (bijv. omdat git de map niet meer als geldige
// worktree herkent), valt het terug op git worktree prune om verweesde
// metadata op te ruimen. Bestaat de map daarna niet meer, dan is het doel
// alsnog bereikt en geeft dit nil terug — opruimen mag geen harde fout
// worden voor de aanroeper.
func WorktreeRemove(ctx context.Context, repoDir, worktreePath string) error {
	_, removeErr := Run(ctx, repoDir, "worktree", "remove", "--force", worktreePath)
	if removeErr == nil {
		return nil
	}
	_, _ = Run(ctx, repoDir, "worktree", "prune")
	if _, statErr := os.Stat(worktreePath); os.IsNotExist(statErr) {
		return nil
	}
	return fmt.Errorf("git worktree remove %s: %w", worktreePath, removeErr)
}
