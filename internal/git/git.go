// Package git wraps the git CLI for the operations action-deployer needs:
// configure identity, stage files, commit, and push with retry.
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
	out, err := c.output("diff", "--cached", "--quiet")
	if err == nil && strings.TrimSpace(out) == "" {
		slog.Info("nothing to commit")
		return nil
	}
	return c.run("commit", "-m", message)
}

// Push pushes to origin/branch with exponential-backoff retry on conflict.
// It pulls --rebase before each retry attempt.
func (c *Client) Push(branch string, maxAttempts int) error {
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := c.run("push", "origin", branch)
		if err == nil {
			return nil
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
