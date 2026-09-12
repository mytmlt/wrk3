// Package source abstracts where worktrees come from.
//
// v1 ships the git source (fetch <remote> --prune, branch -r,
// worktree add/remove/list). New Source types register in registry.go;
// unknown types error listing available options.
package source

// WorktreeInfo describes one entry from `git worktree list --porcelain`.
type WorktreeInfo struct {
	// Path is the absolute worktree path.
	Path string
	// Branch is the short branch name (refs/heads/ prefix stripped).
	// Empty when HEAD is detached.
	Branch string
	// Commit is the HEAD SHA for the worktree.
	Commit string
	// Bare is true when the worktree entry is a bare repository.
	Bare bool
}

// BranchRef is one remote branch with its tip-commit author.
type BranchRef struct {
	// Name is the short branch name (origin/ prefix stripped).
	Name string
	// AuthorName is the tip commit's author name.
	AuthorName string
	// AuthorEmail is the tip commit's author email (brackets stripped).
	AuthorEmail string
}

// Source provisions worktree directories from branches.
type Source interface {
	// Fetch prunes remote refs (git fetch <remote> --prune).
	// Empty remote means the default ("origin").
	Fetch(repoPath, remote string) error
	// Refs lists known remote branches (git branch -r, <remote>/*).
	Refs(repoPath, remote string) ([]string, error)
	// RefsDetailed lists remote branches with tip-commit authors
	// (for-each-ref over refs/remotes/<remote>).
	RefsDetailed(repoPath, remote string) ([]BranchRef, error)
	// Identity returns git config user.name/user.email for repoPath.
	// Empty strings mean unset (no error).
	Identity(repoPath string) (name, email string, err error)
	// Add creates a worktree for branch at worktreePath (git worktree add).
	// When the branch has no local ref but <remote>/<branch> exists, Add
	// creates a tracking branch (--track -b). Empty remote means default.
	Add(repoPath, branch, worktreePath, remote string) error
	// Remove deletes the worktree at worktreePath (git worktree remove).
	Remove(repoPath, worktreePath string, force bool) error
	// List returns existing worktrees (git worktree list --porcelain).
	List(repoPath string) ([]WorktreeInfo, error)
}
