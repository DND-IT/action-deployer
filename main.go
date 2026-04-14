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

	repo := os.Getenv("GITHUB_REPOSITORY") // "owner/repo"
	owner, repoName, _ := strings.Cut(repo, "/")

	opts := deployer.Options{
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
	}

	result, err := deployer.Run(opts)
	if err != nil {
		slog.Error("deploy failed", "error", err)
		os.Exit(1)
	}

	if err := deployer.WriteOutputs(result); err != nil {
		slog.Error("writing outputs", "error", err)
		os.Exit(1)
	}

	slog.Info("done", "deployed", result.Deployed, "environments", result.Environments)
}

// mustInput reads a required INPUT_* env var.
func mustInput(key string) string {
	v := os.Getenv("INPUT_" + key)
	if v == "" {
		slog.Error("required input missing", "input", strings.ToLower(key))
		os.Exit(1)
	}
	return v
}

// inputOrDefault reads an optional INPUT_* env var with a fallback.
func inputOrDefault(key, fallback string) string {
	if v := os.Getenv("INPUT_" + key); v != "" {
		return v
	}
	return fallback
}
