// Package wrapped is a pushgate fixture: every push here is under PushLocal (or
// an exempt GitHub-origin function), so the gate reports nothing.
package wrapped

import (
	"context"
	"os/exec"

	gogit "github.com/go-git/go-git/v5"

	github "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
)

// Merge pushes inside the PushLocal callback, directly and through helpers.
func Merge(ctx context.Context, repo *gogit.Repository, dir string) error {
	return github.PushLocal(ctx, dir, func() error {
		if err := repo.PushContext(ctx, &gogit.PushOptions{}); err != nil {
			return err
		}
		if err := exec.Command("git", "push", "origin", "main").Run(); err != nil {
			return err
		}
		if err := gitRun("push", "origin", "main"); err != nil {
			return err
		}
		return deleteBranch(repo)
	})
}

// deleteBranch is unexported and only ever called under PushLocal.
func deleteBranch(repo *gogit.Repository) error {
	return deepDelete(repo)
}

func deepDelete(repo *gogit.Repository) error {
	return repo.Push(&gogit.PushOptions{})
}

// gitRun is a git runner: it spreads its arguments into exec.Command("git").
func gitRun(args ...string) error {
	return exec.Command("git", append([]string{"-c", "core.hooksPath=/dev/null"}, args...)...).Run()
}

// Status runs a non-push git command through the runner — not a push.
func Status() error { return gitRun("status") }

// PublishToOrigin pushes to a GitHub origin and is named in Spec.Exempt.
func PublishToOrigin(ctx context.Context, repo *gogit.Repository) error {
	return repo.PushContext(ctx, &gogit.PushOptions{RemoteName: "origin"})
}
