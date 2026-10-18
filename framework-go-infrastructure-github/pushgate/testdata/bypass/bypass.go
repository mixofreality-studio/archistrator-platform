// Package bypass is a pushgate fixture: each function below pushes OUTSIDE
// PushLocal in a different way, and the gate must flag every one.
package bypass

import (
	"context"
	"os/exec"

	gogit "github.com/go-git/go-git/v5"

	github "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
)

// DirectPush calls go-git Push with no lock.
func DirectPush(repo *gogit.Repository) error {
	return repo.Push(&gogit.PushOptions{})
}

// RemotePush calls (*Remote).PushContext with no lock.
func RemotePush(ctx context.Context, r *gogit.Remote) error {
	return r.PushContext(ctx, &gogit.PushOptions{})
}

// MethodValue smuggles Push out as a value.
func MethodValue(ctx context.Context, repo *gogit.Repository, dir string) error {
	push := repo.Push
	return github.PushLocal(ctx, dir, func() error { return push(&gogit.PushOptions{}) })
}

// ExecPush shells out to git push with no lock.
func ExecPush(ctx context.Context) error {
	return exec.CommandContext(ctx, "git", "push", "origin", "main").Run()
}

// RunnerPush pushes through a git runner with no lock.
func RunnerPush() error { return gitRun("push", "origin", "main") }

func gitRun(args ...string) error { return exec.Command("git", args...).Run() }

// MixedCaller calls the helper once under the lock and once without.
func MixedCaller(ctx context.Context, repo *gogit.Repository, dir string) error {
	if err := github.PushLocal(ctx, dir, func() error { return helper(repo) }); err != nil {
		return err
	}
	return helper(repo)
}

func helper(repo *gogit.Repository) error { return repo.Push(&gogit.PushOptions{}) }

// NotTheWrapper passes the push to a lookalike that holds no lock.
func NotTheWrapper(repo *gogit.Repository) error {
	return run(func() error { return repo.Push(&gogit.PushOptions{}) })
}

func run(f func() error) error { return f() }

// ExportedHelper is exported, so callers outside the loaded set cannot be proven.
func ExportedHelper(repo *gogit.Repository) error { return repo.Push(&gogit.PushOptions{}) }

// WrapsExported calls the exported helper under the lock — not enough.
func WrapsExported(ctx context.Context, repo *gogit.Repository, dir string) error {
	return github.PushLocal(ctx, dir, func() error { return ExportedHelper(repo) })
}
