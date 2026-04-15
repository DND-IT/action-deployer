// Package git wraps the git CLI for the operations action-deployer needs:
// configure identity, stage/commit/push, branch checkout, and force-push.
package git

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// Client runs git commands in a working directory.
type Client struct {
	Dir       string
	UserName  string
	UserEmail string
}

// Configure sets the local git identity.
func (c *Client) Configure() error {
	if err := c.run("config", "user.name", c.UserName); err != nil {
		return err
	}
	return c.run("config", "user.email", c.UserEmail)
}

// Add stages a file.
func (c *Client) Add(path string) error {
	return c.run("add", path)
}

// Commit creates a commit; returns nil without error if there is nothing to commit.
func (c *Client) Commit(message string) error {
	// `git diff --cached --quiet` exits 0 if no staged changes, 1 if there are changes.
	if _, err := c.output("diff", "--cached", "--quiet"); err == nil {
		slog.Info("nothing to commit")
		return nil
	}
	return c.run("commit", "-m", message)
}

// Push pushes to origin/branch with exponential-backoff retry on conflict.
// Pulls --rebase before each retry attempt. Returns early on auth failures
// (no retry, distinct error message).
func (c *Client) Push(branch string, maxAttempts int) error {
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := c.run("push", "origin", branch)
		if err == nil {
			return nil
		}
		if isAuthFailure(err) {
			return fmt.Errorf("authentication failed — check token has contents:write scope: %w", err)
		}
		if attempt == maxAttempts {
			return fmt.Errorf("push failed after %d attempts: %w", maxAttempts, err)
		}
		delay := time.Duration(attempt*attempt) * time.Second
		slog.Warn("push conflict, retrying", "attempt", attempt, "delay", delay)
		time.Sleep(delay)
		if err := c.run("pull", "--rebase", "origin", branch); err != nil {
			return fmt.Errorf("pull --rebase failed: %w", err)
		}
	}
	return nil
}

// CheckoutBranch creates or resets `branch` to `origin/from` and checks it out.
// Runs: git fetch origin && git checkout -B <branch> origin/<from>
func (c *Client) CheckoutBranch(branch, from string) error {
	if err := c.run("fetch", "origin", from); err != nil {
		return fmt.Errorf("fetching origin/%s: %w", from, err)
	}
	return c.run("checkout", "-B", branch, "origin/"+from)
}

// ForcePush pushes branch to origin with --force. Deploy branches are fully
// owned by the action — another run for the same service/env is the only
// plausible concurrent writer, and in that case "last writer wins" with the
// newest tag is the correct outcome anyway.
func (c *Client) ForcePush(branch string) error {
	err := c.run("push", "origin", branch, "--force")
	if err == nil {
		return nil
	}
	if isAuthFailure(err) {
		return fmt.Errorf("authentication failed — check token has contents:write scope: %w", err)
	}
	return fmt.Errorf("force-push %s: %w", branch, err)
}

// RevParse resolves a ref to its full SHA.
func (c *Client) RevParse(ref string) (string, error) {
	out, err := c.output("rev-parse", ref)
	if err != nil {
		return "", fmt.Errorf("rev-parse %s: %w", ref, err)
	}
	return strings.TrimSpace(out), nil
}

// DefaultBranch reads the remote HEAD symbolic ref (e.g. "main"). Returns ""
// if the ref cannot be resolved (e.g. shallow clone). Callers should fall back
// to GITHUB_DEFAULT_BRANCH.
func (c *Client) DefaultBranch() string {
	out, err := c.output("symbolic-ref", "refs/remotes/origin/HEAD")
	if err != nil {
		return ""
	}
	ref := strings.TrimSpace(out)
	parts := strings.Split(ref, "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

// isAuthFailure matches known git auth failure patterns in combined output.
func isAuthFailure(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, pattern := range []string{
		"403",
		"unable to access",
		"could not read Username",
		"Authentication failed",
		"Permission denied",
	} {
		if strings.Contains(msg, pattern) {
			return true
		}
	}
	return false
}

func (c *Client) run(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = c.Dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	slog.Debug("git", "args", args, "output", strings.TrimSpace(string(out)))
	return nil
}

func (c *Client) output(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = c.Dir
	out, err := cmd.Output()
	return string(out), err
}
