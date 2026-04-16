package main

import (
	"log/slog"
	"os"
	"strings"

	"github.com/DND-IT/action-deployer/internal/deployer"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	if runID := os.Getenv("GITHUB_RUN_ID"); runID != "" {
		slog.SetDefault(slog.Default().With("run_id", runID))
	}

	repo := os.Getenv("GITHUB_REPOSITORY") // "owner/repo"
	owner, repoName, _ := strings.Cut(repo, "/")

	var (
		result *deployer.Result
		err    error
		mode   string
	)

	// Direct mode is triggered by any non-empty line in INPUT_FILE.
	// Otherwise matrix mode.
	if files := splitLines(os.Getenv("INPUT_FILE")); len(files) > 0 {
		mode = "direct"
		result, err = deployer.RunDirect(deployer.DirectOptions{
			Files:         files,
			Value:         mustInput("VALUE"),
			Mode:          inputOrDefault("MODE", "image"),
			Key:           os.Getenv("INPUT_KEY"),
			Deploy:        inputOrDefault("DEPLOY", "auto"),
			Branch:        os.Getenv("INPUT_BRANCH"),
			AutoMerge:     inputOrDefault("AUTO_MERGE", "false") == "true",
			MergeMethod:   inputOrDefault("MERGE_METHOD", "SQUASH"),
			Token:         mustInput("TOKEN"),
			GitUserName:   inputOrDefault("GIT_USER_NAME", "github-actions[bot]"),
			GitUserEmail:  inputOrDefault("GIT_USER_EMAIL", "github-actions[bot]@users.noreply.github.com"),
			DryRun:        inputOrDefault("DRY_RUN", "false") == "true",
			Owner:         owner,
			Repo:          repoName,
			WorkDir:       ".",
			CommitMessage: os.Getenv("INPUT_COMMIT_MESSAGE"),
		})
	} else {
		mode = "matrix"
		result, err = deployer.Run(deployer.Options{
			Service:      mustInput("SERVICE"),
			Version:      mustInput("VERSION"),
			SHA:          mustInput("SHA"),
			Token:        mustInput("TOKEN"),
			ConfigPath:   inputOrDefault("CONFIG", ".github/matrix.config.yaml"),
			ChartsDir:    inputOrDefault("CHARTS_DIR", "deploy/charts"),
			GitUserName:  inputOrDefault("GIT_USER_NAME", "github-actions[bot]"),
			GitUserEmail: inputOrDefault("GIT_USER_EMAIL", "github-actions[bot]@users.noreply.github.com"),
			DryRun:       inputOrDefault("DRY_RUN", "false") == "true",
			Owner:        owner,
			Repo:         repoName,
			WorkDir:      ".",
		})
	}

	version := os.Getenv("INPUT_VERSION")
	if version == "" {
		version = os.Getenv("INPUT_VALUE")
	}

	if err != nil {
		slog.Error("deploy failed", "mode", mode, "error", err)
		_ = deployer.WriteOutputs(result)
		_ = deployer.WriteStepSummary(result, modeLabel(mode), version)
		os.Exit(1)
	}
	if err := deployer.WriteOutputs(result); err != nil {
		slog.Error("writing outputs", "error", err)
		os.Exit(1)
	}
	if err := deployer.WriteStepSummary(result, modeLabel(mode), version); err != nil {
		slog.Warn("writing step summary", "error", err)
	}
	slog.Info("done",
		"mode", mode,
		"deployed", result.Deployed,
		"environments", result.Environments,
		"commit_sha", result.CommitSHA,
		"pr_count", len(result.PRURLs),
	)
}

func modeLabel(mode string) string {
	if mode == "direct" {
		return "direct"
	}
	if s := os.Getenv("INPUT_SERVICE"); s != "" {
		return s
	}
	return "matrix"
}

func mustInput(key string) string {
	v := os.Getenv("INPUT_" + key)
	if v == "" {
		slog.Error("required input missing", "input", strings.ToLower(key))
		os.Exit(1)
	}
	return v
}

func inputOrDefault(key, fallback string) string {
	if v := os.Getenv("INPUT_" + key); v != "" {
		return v
	}
	return fallback
}

// splitLines splits a newline-separated input into trimmed, non-empty lines.
// Used for multi-value inputs like INPUT_FILE.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for line := range strings.SplitSeq(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}
