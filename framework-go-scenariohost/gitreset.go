package scenariohost

import (
	"fmt"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// resetGitRepos puts every bare repository the host keeps (the shared Repo and each Input/GitRepo
// one) back to the state startLocalGitRepo created: main at the seed commit and no other branch.
//
// A scenario runs against a repository the scenarios before it in the package have already
// written to, and a component that guards its repository's identity (a project-state repo refuses
// a second project) sees a scenario that arranged one project as contamination for the next
// scenario's different project. Each scenario therefore starts from the seed.
func (h *Host) resetGitRepos() error {
	h.gitMu.Lock()
	defer h.gitMu.Unlock()
	dirs := make([]string, 0, 1+len(h.gitRepos))
	if h.Repo.Dir != "" {
		dirs = append(dirs, h.Repo.Dir)
	}
	for _, repo := range h.gitRepos {
		dirs = append(dirs, repo.Dir)
	}
	for _, dir := range dirs {
		if err := resetBareRepo(dir, "main"); err != nil {
			return fmt.Errorf("reset %s: %w", dir, err)
		}
	}
	return nil
}

// resetBareRepo points branch at the root commit of its history (the seed) and deletes every
// other branch and tag. Objects it leaves behind are unreachable and harmless.
func resetBareRepo(dir, branch string) error {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return err
	}
	ref := plumbing.NewBranchReferenceName(branch)
	root, err := rootCommit(repo, ref)
	if err != nil {
		return err
	}
	others, err := otherRefs(repo, ref)
	if err != nil {
		return err
	}
	for _, name := range others {
		if err := repo.Storer.RemoveReference(name); err != nil {
			return err
		}
	}
	return repo.Storer.SetReference(plumbing.NewHashReference(ref, root))
}

// rootCommit follows ref's first parents back to the commit with none.
func rootCommit(repo *git.Repository, ref plumbing.ReferenceName) (plumbing.Hash, error) {
	tip, err := repo.Reference(ref, true)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("resolve %s: %w", ref, err)
	}
	commit, err := repo.CommitObject(tip.Hash())
	if err != nil {
		return plumbing.ZeroHash, err
	}
	for commit.NumParents() > 0 {
		if commit, err = commit.Parent(0); err != nil {
			return plumbing.ZeroHash, err
		}
	}
	return commit.Hash, nil
}

// otherRefs lists every hash reference but keep.
func otherRefs(repo *git.Repository, keep plumbing.ReferenceName) ([]plumbing.ReferenceName, error) {
	refs, err := repo.References()
	if err != nil {
		return nil, err
	}
	var names []plumbing.ReferenceName
	err = refs.ForEach(func(r *plumbing.Reference) error {
		if r.Type() == plumbing.HashReference && r.Name() != keep {
			names = append(names, r.Name())
		}
		return nil
	})
	return names, err
}
