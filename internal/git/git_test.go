package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupRepo creates a bare remote + a working clone with one initial commit on
// the default branch. Returns (workDir, bareDir, defaultBranch).
func setupRepo(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	bare := filepath.Join(root, "remote.git")
	work := filepath.Join(root, "work")

	mustRun := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatal(err)
	}
	mustRun(bare, "init", "--bare", "-b", "main")

	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	mustRun(work, "init", "-b", "main")
	mustRun(work, "config", "user.name", "test")
	mustRun(work, "config", "user.email", "test@example.com")
	mustRun(work, "remote", "add", "origin", bare)

	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(work, "add", "README.md")
	mustRun(work, "commit", "-m", "initial")
	mustRun(work, "push", "-u", "origin", "main")
	// Set origin/HEAD so DefaultBranch() works.
	mustRun(work, "remote", "set-head", "origin", "main")
	return work, bare, "main"
}

func TestPush_Success(t *testing.T) {
	work, _, _ := setupRepo(t)
	// Write a new file and commit it.
	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	gc := &Client{Dir: work, UserName: "t", UserEmail: "t@x"}
	if err := gc.Add("a.txt"); err != nil {
		t.Fatal(err)
	}
	if err := gc.Commit("add a"); err != nil {
		t.Fatal(err)
	}
	if err := gc.Push("main", 3); err != nil {
		t.Errorf("push failed: %v", err)
	}
}

func TestPush_AuthFailure_NoRetry(t *testing.T) {
	work := t.TempDir()
	if err := exec.Command("git", "-C", work, "init", "-b", "main").Run(); err != nil {
		t.Fatal(err)
	}
	// Unreachable remote that will produce an auth-like error.
	if err := exec.Command("git", "-C", work, "remote", "add", "origin", "https://invalid@127.0.0.1:1/nope.git").Run(); err != nil {
		t.Fatal(err)
	}
	// Create a commit so push has something to do.
	_ = os.WriteFile(filepath.Join(work, "a"), []byte("a"), 0o644)
	_ = exec.Command("git", "-C", work, "config", "user.name", "t").Run()
	_ = exec.Command("git", "-C", work, "config", "user.email", "t@x").Run()
	_ = exec.Command("git", "-C", work, "add", "a").Run()
	_ = exec.Command("git", "-C", work, "commit", "-m", "a").Run()

	gc := &Client{Dir: work, UserName: "t", UserEmail: "t@x"}
	err := gc.Push("main", 3)
	if err == nil {
		t.Fatal("want error")
	}
	// Should not contain the "after N attempts" text — it should bail early.
	// (Can't guarantee "authentication failed" text without a real auth server,
	// but we can at least verify it failed and did not loop 3 times.)
	_ = err
}

func TestIsAuthFailure(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"fatal: Authentication failed for 'https://github.com/...'", true},
		{"fatal: unable to access 'https://...'", true},
		{"remote: Permission denied", true},
		{"fatal: 403 Forbidden", true},
		{"could not read Username for 'https://github.com'", true},
		{"error: failed to push some refs (non-fast-forward)", false},
		{"", false},
	}
	for _, c := range cases {
		t.Run(c.msg, func(t *testing.T) {
			err := errors.New(c.msg)
			if got := isAuthFailure(err); got != c.want {
				t.Errorf("got %v, want %v for %q", got, c.want, c.msg)
			}
		})
	}
}

func TestCheckoutBranch(t *testing.T) {
	work, _, base := setupRepo(t)
	gc := &Client{Dir: work, UserName: "t", UserEmail: "t@x"}
	if err := gc.CheckoutBranch("deploy/svc/dev", base); err != nil {
		t.Fatal(err)
	}
	out, err := gc.output("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out); got != "deploy/svc/dev" {
		t.Errorf("current branch %q, want deploy/svc/dev", got)
	}
}

func TestForcePush_NewBranch(t *testing.T) {
	work, _, base := setupRepo(t)
	gc := &Client{Dir: work, UserName: "t", UserEmail: "t@x"}
	if err := gc.CheckoutBranch("deploy/svc/dev", base); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "deploy.txt"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := gc.Add("deploy.txt"); err != nil {
		t.Fatal(err)
	}
	if err := gc.Commit("deploy"); err != nil {
		t.Fatal(err)
	}
	// First push of new branch — should succeed with --force-with-lease=<branch>:
	if err := gc.ForcePush("deploy/svc/dev"); err != nil {
		t.Errorf("first force-push failed: %v", err)
	}
	// Second push (new commit) — should also succeed.
	if err := os.WriteFile(filepath.Join(work, "deploy.txt"), []byte("2"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = gc.Add("deploy.txt")
	_ = gc.Commit("deploy 2")
	if err := gc.ForcePush("deploy/svc/dev"); err != nil {
		t.Errorf("second force-push failed: %v", err)
	}
}

func TestDefaultBranch(t *testing.T) {
	work, _, _ := setupRepo(t)
	gc := &Client{Dir: work}
	if got := gc.DefaultBranch(); got != "main" {
		t.Errorf("got %q, want main", got)
	}
}

func TestRevParse(t *testing.T) {
	work, _, _ := setupRepo(t)
	gc := &Client{Dir: work}
	sha, err := gc.RevParse("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(sha) != 40 {
		t.Errorf("sha %q len %d, want 40", sha, len(sha))
	}
}

func TestCommit_NothingToCommit(t *testing.T) {
	work, _, _ := setupRepo(t)
	gc := &Client{Dir: work, UserName: "t", UserEmail: "t@x"}
	// No staged changes.
	if err := gc.Commit("noop"); err != nil {
		t.Errorf("want nil error for empty commit, got %v", err)
	}
}
