// Package deployer orchestrates the full deploy flow:
// resolve environments → update values files → atomic commit OR create PRs.
package deployer

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/DND-IT/action-deployer/internal/config"
	"github.com/DND-IT/action-deployer/internal/git"
	gh "github.com/DND-IT/action-deployer/internal/github"
	"github.com/DND-IT/action-deployer/internal/values"
)

// Options holds all inputs for a deploy run.
type Options struct {
	Service      string
	Version      string
	SHA          string
	Token        string
	ConfigPath   string
	ChartsDir    string
	GitUserName  string
	GitUserEmail string
	DryRun       bool
	// Resolved from GITHUB_REPOSITORY env var
	Owner string
	Repo  string
	// Working directory (defaults to current directory)
	WorkDir string
}

// Result summarises what was done.
type Result struct {
	Deployed     bool
	Environments []string
}

// Run executes the full deploy flow.
func Run(opts Options) (*Result, error) {
	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return nil, err
	}

	envs, err := cfg.Resolve(opts.Service)
	if err != nil {
		return nil, err
	}

	var (
		autoEnvs []config.Environment
		prEnvs   []config.Environment
	)
	for _, e := range envs {
		switch e.Deploy {
		case "auto":
			autoEnvs = append(autoEnvs, e)
		case "pr":
			prEnvs = append(prEnvs, e)
		default:
			slog.Warn("unknown deploy type, skipping", "environment", e.Name, "deploy", e.Deploy)
		}
	}

	result := &Result{}

	if err := deployAuto(opts, autoEnvs, result); err != nil {
		return nil, err
	}
	if err := deployPR(opts, prEnvs, result); err != nil {
		return nil, err
	}

	return result, nil
}

func deployAuto(opts Options, envs []config.Environment, result *Result) error {
	if len(envs) == 0 {
		return nil
	}

	gc := &git.Client{
		Dir:       opts.WorkDir,
		UserName:  opts.GitUserName,
		UserEmail: opts.GitUserEmail,
	}

	if !opts.DryRun {
		if err := gc.Configure(); err != nil {
			return err
		}
	}

	for _, e := range envs {
		tag := resolveTag(e, opts)
		file := valuesPath(opts, e.Name)

		slog.Info("updating values file", "environment", e.Name, "file", file, "tag", tag)

		if opts.DryRun {
			slog.Info("[dry-run] would update", "file", file, "tag", tag)
			result.Environments = append(result.Environments, e.Name)
			continue
		}

		if err := values.SetImageTag(file, tag); err != nil {
			return fmt.Errorf("environment %s: %w", e.Name, err)
		}
		if err := gc.Add(file); err != nil {
			return err
		}
		result.Environments = append(result.Environments, e.Name)
	}

	if opts.DryRun {
		result.Deployed = len(result.Environments) > 0
		return nil
	}

	msg := fmt.Sprintf("deploy(%s): %s", opts.Service, opts.Version)
	if err := gc.Commit(msg); err != nil {
		return err
	}

	branch := currentBranch(opts.WorkDir)
	if err := gc.Push(branch, 3); err != nil {
		return fmt.Errorf("push failed — this may be a concurrent deploy from another service pipeline: %w", err)
	}

	result.Deployed = len(result.Environments) > 0
	return nil
}

func deployPR(opts Options, envs []config.Environment, result *Result) error {
	if len(envs) == 0 {
		return nil
	}

	ghc := gh.NewClient(opts.Token, opts.Owner, opts.Repo)

	for _, e := range envs {
		tag := resolveTag(e, opts)
		file := valuesPath(opts, e.Name)
		branch := fmt.Sprintf("deploy/%s/%s", opts.Service, e.Name)
		title := fmt.Sprintf("deploy(%s/%s): %s", opts.Service, e.Name, opts.Version)

		slog.Info("creating deploy PR", "environment", e.Name, "branch", branch, "tag", tag)

		if opts.DryRun {
			slog.Info("[dry-run] would create PR", "branch", branch, "title", title)
			result.Environments = append(result.Environments, e.Name)
			continue
		}

		// Update the values file on the deploy branch, then open/update the PR.
		// TODO: implement branch push + PR flow (push values change to branch, then EnsurePR)
		_ = file
		prURL, err := ghc.EnsurePR(branch, defaultBranch(opts.WorkDir), title, "", []string{"deploy"})
		if err != nil {
			return fmt.Errorf("environment %s: %w", e.Name, err)
		}
		slog.Info("PR ready", "url", prURL)
		result.Environments = append(result.Environments, e.Name)
		result.Deployed = true
	}
	return nil
}

func resolveTag(e config.Environment, opts Options) string {
	if e.Tag == "sha" {
		return opts.SHA
	}
	return opts.Version
}

func valuesPath(opts Options, env string) string {
	return filepath.Join(opts.ChartsDir, opts.Service, "envs", env, "values.yaml")
}

func currentBranch(dir string) string {
	// TODO: read from git or GITHUB_REF_NAME env var
	if b := os.Getenv("GITHUB_REF_NAME"); b != "" {
		return b
	}
	return "main"
}

func defaultBranch(dir string) string {
	if b := os.Getenv("GITHUB_BASE_REF"); b != "" {
		return b
	}
	return "main"
}

// WriteOutputs writes GitHub Actions outputs.
func WriteOutputs(result *Result) error {
	outputFile := os.Getenv("GITHUB_OUTPUT")
	if outputFile == "" {
		return nil
	}

	envsJSON, err := json.Marshal(result.Environments)
	if err != nil {
		return err
	}

	deployed := "false"
	if result.Deployed {
		deployed = "true"
	}

	content := strings.Join([]string{
		"deployed=" + deployed,
		"environments=" + string(envsJSON),
	}, "\n") + "\n"

	f, err := os.OpenFile(outputFile, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(content)
	return err
}
