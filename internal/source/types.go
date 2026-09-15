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

// BranchRef is one remote branch with its tip-commit author and committer.
type BranchRef struct {
	// Name is the short branch name (origin/ prefix stripped).
	Name string
	// AuthorName is the tip commit's author name.
	AuthorName string
	// AuthorEmail is the tip commit's author email (brackets stripped).
	AuthorEmail string
	// CommitterName is the tip commit's committer name.
	CommitterName string
	// CommitterEmail is the tip commit's committer email (brackets stripped).
	CommitterEmail string
}

// MineHistoryLimit caps how many branch-exclusive commits --mine/--author
// scan per branch. Cursor/bot branches you pushed usually carry your
// commit within a handful of exclusive commits; 100 keeps per-branch
// `git log` output small even on repos with hundreds of remote branches.
const MineHistoryLimit = 100

// Source provisions worktree directories from branches.
type Source interface {
	// Fetch prunes remote refs (git fetch <remote> --prune).
	// Empty remote means the default ("origin").
	Fetch(repoPath, remote string) error
	// Refs lists known remote branches (git branch -r, <remote>/*).
	Refs(repoPath, remote string) ([]string, error)
	// LocalBranches lists local branch names (git branch, refs/heads/*).
	// Used to offer worktree creation from branches that exist only
	// locally; unlike Refs it needs no network fetch.
	LocalBranches(repoPath string) ([]string, error)
	// RefsDetailed lists remote branches with tip-commit authors and
	// committers (for-each-ref over refs/remotes/<remote>).
	RefsDetailed(repoPath, remote string) ([]BranchRef, error)
	// DefaultBranch returns the short name of the remote's default branch
	// (e.g. "main"). Empty means unknown — callers must fall back to
	// tip-only matching (scanning full history without a base would match
	// mainline commits and flag every branch).
	DefaultBranch(repoPath, remote string) (string, error)
	// BranchHistory lists up to limit branch-exclusive commits (newest
	// first) as BranchRefs (Name is the branch; author/committer fields
	// describe each commit). Base is the short default-branch name from
	// DefaultBranch; empty base means plain `log <remote>/<branch>`
	// with no exclusion. Limit <= 0 means MineHistoryLimit.
	BranchHistory(repoPath, remote, branch, base string, limit int) ([]BranchRef, error)
	// Identity returns git config user.name/user.email for repoPath.
	// Empty strings mean unset (no error).
	Identity(repoPath string) (name, email string, err error)
	// Add creates a worktree for branch at worktreePath (git worktree add).
	// When the branch has no local ref but <remote>/<branch> exists, Add
	// creates a tracking branch (--track -b). Empty remote means default.
	Add(repoPath, branch, worktreePath, remote string) error
	// AddNew creates a worktree at worktreePath with a NEW local branch
	// starting at base (git worktree add -b <branch> <worktreePath>
	// <base>). Base is a start point like "<remote>/<default>" or "HEAD".
	AddNew(repoPath, branch, worktreePath, base string) error
	// Remove deletes the worktree at worktreePath (git worktree remove).
	Remove(repoPath, worktreePath string, force bool) error
	// List returns existing worktrees (git worktree list --porcelain).
	List(repoPath string) ([]WorktreeInfo, error)
}
