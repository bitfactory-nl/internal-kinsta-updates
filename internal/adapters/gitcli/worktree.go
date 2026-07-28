package gitcli

import (
	"context"
	"fmt"
	"os"
)

// WorktreeAdd voegt op worktreePath een worktree toe op branch, gezet op
// fromRef. Zo kan een core-update geïsoleerd gebouwd worden zonder de
// bestaande checkout van de gebruiker aan te raken.
//
// Er wordt bewust -B (in plaats van -b) gebruikt: een branch die van een
// eerdere, afgebroken poging is achtergebleven wordt dan gereset naar fromRef
// in plaats van de hele actie te laten falen op "branch already exists". Dat
// mag hier, omdat deze branches door de tool zelf worden aangemaakt en altijd
// vanaf de actuele release-branch horen te beginnen; er wordt nooit geforceerd
// gepusht, dus een remote branch met eigen commits blijft beschermd (de push
// faalt dan met een duidelijke non-fast-forward fout).
func WorktreeAdd(ctx context.Context, repoDir, worktreePath, branch, fromRef string) error {
	_, err := Run(ctx, repoDir, "worktree", "add", "-B", branch, worktreePath, fromRef)
	if err != nil {
		return fmt.Errorf("git worktree add %s %s: %w", branch, worktreePath, err)
	}
	return nil
}

// WorktreePrune ruimt verweesde worktree-registraties op. Nodig wanneer een
// eerdere run hard is afgebroken: de map is dan verdwenen terwijl git de
// worktree nog geregistreerd heeft, wat een nieuwe poging op hetzelfde pad
// blokkeert ("missing but already registered worktree").
func WorktreePrune(ctx context.Context, repoDir string) error {
	if _, err := Run(ctx, repoDir, "worktree", "prune"); err != nil {
		return fmt.Errorf("git worktree prune: %w", err)
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
