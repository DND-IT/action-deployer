package deployer

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	gh "github.com/DND-IT/action-deployer/internal/github"
)

// repoFixture sets up a bare remote + working clone with a matrix.config.yaml
// and per-env values files. Returns (workDir, bareDir).
func repoFixture(t *testing.T, matrixConfig string, envs map[string]string) (string, string) {
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

	_ = os.MkdirAll(bare, 0o755)
	mustRun(bare, "init", "--bare", "-b", "main")

	_ = os.MkdirAll(work, 0o755)
	mustRun(work, "init", "-b", "main")
	mustRun(work, "config", "user.name", "test")
	mustRun(work, "config", "user.email", "t@x")
	mustRun(work, "remote", "add", "origin", bare)

	// Write matrix config.
	cfgPath := filepath.Join(work, "matrix.config.yaml")
	if err := os.WriteFile(cfgPath, []byte(matrixConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write per-env values files.
	for envName, content := range envs {
		p := filepath.Join(work, "charts", "svc-a", "envs", envName, "values.yaml")
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mustRun(work, "add", ".")
	mustRun(work, "commit", "-m", "initial")
	mustRun(work, "push", "-u", "origin", "main")
	mustRun(work, "remote", "set-head", "origin", "main")
	return work, bare
}

const valuesYAML = `image:
  repository: myrepo/svc-a
  tag: 1.0.0
replicas: 1
`

func TestDeployAuto_Basic(t *testing.T) {
	matrix := `
global:
  charts_dir: charts
environment:
  dev:
    deploy: auto
    tag: version
service:
  svc-a:
    deploy: auto
`
	work, bare := repoFixture(t, matrix, map[string]string{"dev": valuesYAML})

	t.Setenv("GITHUB_REF_NAME", "main")
	opts := Options{
		Service:    "svc-a",
		Version:    "2.0.0",
		SHA:        "abc1234",
		Token:      "ignored",
		ConfigPath:   filepath.Join(work, "matrix.config.yaml"),
		ChartsDir:    filepath.Join(work, "charts"),
		GitUserName:  "test-bot",
		GitUserEmail: "test-bot@example.com",
		WorkDir:      work,
	}
	result, err := Run(opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Deployed {
		t.Errorf("Deployed=false, want true")
	}
	if len(result.Environments) != 1 || result.Environments[0] != "dev" {
		t.Errorf("Environments=%v, want [dev]", result.Environments)
	}
	if result.CommitSHA == "" {
		t.Error("CommitSHA empty")
	}
	// Verify the commit is pushed to remote.
	cmd := exec.Command("git", "-C", bare, "log", "main", "--oneline")
	out, _ := cmd.CombinedOutput()
	if !strings.Contains(string(out), "deploy(svc-a): 2.0.0") {
		t.Errorf("deploy commit not pushed:\n%s", out)
	}
	// Verify the tag was updated in the working tree.
	got, _ := os.ReadFile(filepath.Join(work, "charts", "svc-a", "envs", "dev", "values.yaml"))
	if !strings.Contains(string(got), "tag: 2.0.0") {
		t.Errorf("values file not updated:\n%s", got)
	}
	// Verify diff summary.
	if len(result.DiffSummary) != 1 {
		t.Fatalf("DiffSummary len=%d, want 1", len(result.DiffSummary))
	}
	if result.DiffSummary[0].OldValue != "1.0.0" || result.DiffSummary[0].NewValue != "2.0.0" {
		t.Errorf("DiffSummary=%+v", result.DiffSummary[0])
	}
}

func TestDeployAuto_DryRun(t *testing.T) {
	matrix := `
environment:
  dev:
    deploy: auto
    tag: version
service:
  svc-a:
    deploy: auto
`
	work, _ := repoFixture(t, matrix, map[string]string{"dev": valuesYAML})

	opts := Options{
		Service:    "svc-a",
		Version:    "2.0.0",
		SHA:        "abc",
		Token:      "ignored",
		ConfigPath:   filepath.Join(work, "matrix.config.yaml"),
		ChartsDir:    filepath.Join(work, "charts"),
		GitUserName:  "test-bot",
		GitUserEmail: "test-bot@example.com",
		WorkDir:      work,
		DryRun:     true,
	}
	result, err := Run(opts)
	if err != nil {
		t.Fatal(err)
	}
	if result.Deployed {
		t.Error("dry-run: want Deployed=false, got true")
	}
	if len(result.Environments) != 1 {
		t.Errorf("Environments=%v", result.Environments)
	}
	// Values file should be unchanged.
	got, _ := os.ReadFile(filepath.Join(work, "charts", "svc-a", "envs", "dev", "values.yaml"))
	if !strings.Contains(string(got), "tag: 1.0.0") {
		t.Errorf("dry-run modified file:\n%s", got)
	}
}

// fakeGH returns a minimal GitHub API for PR integration tests.
type fakeGH struct {
	*httptest.Server
	prCount     atomic.Int32
	graphqlHits atomic.Int32
}

func newFakeGH(t *testing.T) *fakeGH {
	s := &fakeGH{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch {
		case r.URL.Path == "/graphql":
			s.graphqlHits.Add(1)
			if !strings.Contains(string(body), "enablePullRequestAutoMerge") {
				t.Errorf("graphql body missing mutation: %s", body)
			}
			_, _ = w.Write([]byte(`{"data":{"enablePullRequestAutoMerge":{"pullRequest":{"id":"x"}}}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "state=open"):
			_ = json.NewEncoder(w).Encode([]gh.PullRequest{})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pulls"):
			n := s.prCount.Add(1)
			_ = json.NewEncoder(w).Encode(gh.PullRequest{
				Number: int(n),
				URL:    "https://example.com/pr/" + string(rune('0'+n)),
				NodeID: "PR_NODE_1",
			})
		case strings.Contains(r.URL.Path, "/labels"):
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected: %s %s", r.Method, r.URL)
		}
	}))
	return s
}

func TestDeployPR_FullFlow(t *testing.T) {
	matrix := `
environment:
  prod:
    deploy: pr
    tag: version
    auto_merge: true
service:
  svc-a:
    deploy: pr
`
	work, bare := repoFixture(t, matrix, map[string]string{"prod": valuesYAML})
	srv := newFakeGH(t)
	defer srv.Close()

	opts := Options{
		Service:       "svc-a",
		Version:       "2.0.0",
		SHA:           "abc",
		Token:         "tok",
		ConfigPath:    filepath.Join(work, "matrix.config.yaml"),
		ChartsDir:     filepath.Join(work, "charts"),
		GitUserName:   "bot",
		GitUserEmail:  "bot@x",
		Owner:         "owner",
		Repo:          "repo",
		WorkDir:       work,
		GitHubBaseURL: srv.URL,
	}
	result, err := Run(opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Deployed {
		t.Error("Deployed=false")
	}
	if len(result.PRURLs) != 1 {
		t.Errorf("PRURLs=%v, want 1", result.PRURLs)
	}
	if srv.prCount.Load() != 1 {
		t.Errorf("want 1 PR created, got %d", srv.prCount.Load())
	}
	if srv.graphqlHits.Load() != 1 {
		t.Errorf("want 1 auto-merge call, got %d", srv.graphqlHits.Load())
	}
	// Deploy branch should exist on the remote with the new tag.
	cmd := exec.Command("git", "-C", bare, "show", "deploy/svc-a/prod:charts/svc-a/envs/prod/values.yaml")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("show deploy branch: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "tag: 2.0.0") {
		t.Errorf("deploy branch does not have updated tag:\n%s", out)
	}
}

func TestDeployPR_DryRun(t *testing.T) {
	matrix := `
environment:
  prod:
    deploy: pr
    tag: version
service:
  svc-a:
    deploy: pr
`
	work, _ := repoFixture(t, matrix, map[string]string{"prod": valuesYAML})

	opts := Options{
		Service:    "svc-a",
		Version:    "2.0.0",
		SHA:        "abc",
		Token:      "ignored",
		ConfigPath: filepath.Join(work, "matrix.config.yaml"),
		ChartsDir:  filepath.Join(work, "charts"),
		Owner:      "owner",
		Repo:       "repo",
		WorkDir:    work,
		DryRun:     true,
	}
	result, err := Run(opts)
	if err != nil {
		t.Fatal(err)
	}
	if result.Deployed {
		t.Error("dry-run PR: want Deployed=false")
	}
	if len(result.Environments) != 1 || result.Environments[0] != "prod" {
		t.Errorf("Environments=%v", result.Environments)
	}
	if len(result.DiffSummary) != 1 || result.DiffSummary[0].OldValue != "1.0.0" {
		t.Errorf("DiffSummary=%+v", result.DiffSummary)
	}
}

func TestValuesPath_Traversal(t *testing.T) {
	work := t.TempDir()
	charts := filepath.Join(work, "charts")
	_ = os.MkdirAll(charts, 0o755)
	opts := Options{Service: "svc", ChartsDir: charts}
	_, err := valuesPath(opts, "../../../etc")
	if err == nil {
		t.Fatal("want error for path traversal, got nil")
	}
	if !strings.Contains(err.Error(), "escapes") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBuildPRBody(t *testing.T) {
	opts := Options{Service: "svc-a", Owner: "o", Repo: "r"}
	t.Setenv("GITHUB_RUN_ID", "99")
	body := buildPRBody(opts, "prod", "1.0.0", "2.0.0")
	for _, want := range []string{
		"## Deploy: svc-a/prod",
		"`1.0.0`",
		"`2.0.0`",
		"https://github.com/o/r/actions/runs/99",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

func TestBuildPRBody_NoOldTag(t *testing.T) {
	opts := Options{Service: "svc", Owner: "o", Repo: "r"}
	body := buildPRBody(opts, "dev", "", "2.0.0")
	if !strings.Contains(body, "_(none)_") {
		t.Errorf("missing (none) marker:\n%s", body)
	}
}

func TestWriteOutputs(t *testing.T) {
	outFile := filepath.Join(t.TempDir(), "output")
	t.Setenv("GITHUB_OUTPUT", outFile)
	r := &Result{
		Deployed:     true,
		Environments: []string{"dev", "staging"},
		CommitSHA:    "abc1234",
		PRURLs:       []string{"https://example.com/pr/1"},
		DiffSummary: []FileDiff{
			{File: "charts/svc/envs/dev/values.yaml", OldValue: "1.0.0", NewValue: "2.0.0"},
		},
	}
	if err := WriteOutputs(r); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(outFile)
	s := string(got)
	for _, want := range []string{"deployed=true", "environments=", "commit_sha=abc1234", "dev", "2.0.0"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in output:\n%s", want, s)
		}
	}
}
