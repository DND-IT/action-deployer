package deployer

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// directFixture creates a bare remote + working clone with a single values
// file. No matrix.config.yaml. Returns (workDir, bareDir, fileRelPath).
func directFixture(t *testing.T, fileRel, content string) (string, string, string) {
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

	fullPath := filepath.Join(work, fileRel)
	_ = os.MkdirAll(filepath.Dir(fullPath), 0o755)
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(work, "add", ".")
	mustRun(work, "commit", "-m", "initial")
	mustRun(work, "push", "-u", "origin", "main")
	mustRun(work, "remote", "set-head", "origin", "main")
	return work, bare, fileRel
}

func TestRunDirect_Auto_ImageMode(t *testing.T) {
	work, bare, file := directFixture(t, "app/values.yaml", valuesYAML)

	t.Setenv("GITHUB_REF_NAME", "main")
	result, err := RunDirect(DirectOptions{
		Files:        []string{file},
		Value:        "3.0.0",
		Mode:         "image",
		Token:        "tok",
		GitUserName:  "bot",
		GitUserEmail: "bot@x",
		WorkDir:      work,
		Deploy:       "auto",
	})
	if err != nil {
		t.Fatalf("RunDirect: %v", err)
	}
	if !result.Deployed {
		t.Error("Deployed=false")
	}
	if result.CommitSHA == "" {
		t.Error("CommitSHA empty")
	}
	// Verify file updated in working tree.
	got, _ := os.ReadFile(filepath.Join(work, file))
	if !strings.Contains(string(got), "tag: 3.0.0") {
		t.Errorf("tag not updated:\n%s", got)
	}
	// Verify commit pushed to remote.
	out, _ := exec.Command("git", "-C", bare, "log", "main", "--oneline").CombinedOutput()
	if !strings.Contains(string(out), "values.yaml") {
		t.Errorf("commit not pushed:\n%s", out)
	}
}

func TestRunDirect_Auto_KeyMode(t *testing.T) {
	work, _, file := directFixture(t, "config.yaml", "replicas: 1\napp:\n  image:\n    tag: old\n")

	_, err := RunDirect(DirectOptions{
		Files:        []string{file},
		Value:        "v2",
		Mode:         "key",
		Key:          "app.image.tag",
		Token:        "tok",
		GitUserName:  "bot",
		GitUserEmail: "bot@x",
		WorkDir:      work,
		Deploy:       "auto",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(work, file))
	if !strings.Contains(string(got), "tag: v2") {
		t.Errorf("key mode failed:\n%s", got)
	}
}

func TestRunDirect_DryRun(t *testing.T) {
	work, _, file := directFixture(t, "app/values.yaml", valuesYAML)

	result, err := RunDirect(DirectOptions{
		Files:   []string{file},
		Value:   "9.9.9",
		Mode:    "image",
		Token:   "tok",
		WorkDir: work,
		Deploy:  "auto",
		DryRun:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Deployed {
		t.Error("dry-run: Deployed should be false")
	}
	if len(result.DiffSummary) != 1 || result.DiffSummary[0].OldValue != "1.0.0" {
		t.Errorf("DiffSummary=%+v", result.DiffSummary)
	}
	// Working tree must be unchanged.
	got, _ := os.ReadFile(filepath.Join(work, file))
	if !strings.Contains(string(got), "tag: 1.0.0") {
		t.Errorf("dry-run modified file:\n%s", got)
	}
}

func TestRunDirect_PR(t *testing.T) {
	work, bare, file := directFixture(t, "app/values.yaml", valuesYAML)
	srv := newFakeGH(t)
	defer srv.Close()

	result, err := RunDirect(DirectOptions{
		Files:         []string{file},
		Value:         "5.5.5",
		Mode:          "image",
		Deploy:        "pr",
		Branch:        "updates/app",
		AutoMerge:     true,
		Token:         "tok",
		Owner:         "owner",
		Repo:          "repo",
		GitUserName:   "bot",
		GitUserEmail:  "bot@x",
		WorkDir:       work,
		GitHubBaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("RunDirect: %v", err)
	}
	if !result.Deployed {
		t.Error("Deployed=false")
	}
	if len(result.PRURLs) != 1 {
		t.Errorf("PRURLs=%v", result.PRURLs)
	}
	if srv.graphqlHits.Load() != 1 {
		t.Errorf("want 1 auto-merge call, got %d", srv.graphqlHits.Load())
	}
	// Verify deploy branch on remote has the update.
	out, err := exec.Command("git", "-C", bare, "show", "updates/app:"+file).CombinedOutput()
	if err != nil {
		t.Fatalf("show deploy branch: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "tag: 5.5.5") {
		t.Errorf("deploy branch missing update:\n%s", out)
	}
}

func TestRunDirect_Validation(t *testing.T) {
	cases := []struct {
		name string
		opts DirectOptions
		want string
	}{
		{"missing files", DirectOptions{Value: "x"}, "at least one file"},
		{"missing value", DirectOptions{Files: []string{"x.yaml"}}, "value is required"},
		{"key mode without key", DirectOptions{Files: []string{"x.yaml"}, Value: "x", Mode: "key"}, "key is required"},
		{"pr without branch", DirectOptions{Files: []string{"x.yaml"}, Value: "x", Deploy: "pr"}, "branch is required"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := RunDirect(c.opts)
			if err == nil {
				t.Fatal("want error, got nil")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error=%q, want contains %q", err, c.want)
			}
		})
	}
}

func TestRunDirect_MultipleFiles_Auto(t *testing.T) {
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
	mustRun(work, "config", "user.name", "t")
	mustRun(work, "config", "user.email", "t@x")
	mustRun(work, "remote", "add", "origin", bare)

	paths := []string{"dev/values.yaml", "prod/values.yaml", "staging/values.yaml"}
	for _, p := range paths {
		full := filepath.Join(work, p)
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(valuesYAML), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustRun(work, "add", ".")
	mustRun(work, "commit", "-m", "init")
	mustRun(work, "push", "-u", "origin", "main")
	mustRun(work, "remote", "set-head", "origin", "main")

	t.Setenv("GITHUB_REF_NAME", "main")
	result, err := RunDirect(DirectOptions{
		Files:        paths,
		Value:        "9.9.9",
		Token:        "tok",
		GitUserName:  "bot",
		GitUserEmail: "bot@x",
		WorkDir:      work,
		Deploy:       "auto",
	})
	if err != nil {
		t.Fatalf("RunDirect: %v", err)
	}
	if !result.Deployed {
		t.Error("Deployed=false")
	}
	if len(result.DiffSummary) != 3 {
		t.Errorf("DiffSummary len=%d, want 3", len(result.DiffSummary))
	}
	// Every file updated.
	for _, p := range paths {
		got, _ := os.ReadFile(filepath.Join(work, p))
		if !strings.Contains(string(got), "tag: 9.9.9") {
			t.Errorf("%s not updated:\n%s", p, got)
		}
	}
	// Only ONE commit on remote (atomic).
	out, _ := exec.Command("git", "-C", bare, "log", "main", "--oneline").CombinedOutput()
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	// 1 init + 1 deploy = 2 commits
	if len(lines) != 2 {
		t.Errorf("want 2 commits on remote, got %d:\n%s", len(lines), out)
	}
	// Commit message mentions "3 files".
	if !strings.Contains(string(out), "3 files") {
		t.Errorf("commit message missing multi-file marker:\n%s", out)
	}
}

func TestRunDirect_MultipleFiles_PR(t *testing.T) {
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
	mustRun(work, "config", "user.name", "t")
	mustRun(work, "config", "user.email", "t@x")
	mustRun(work, "remote", "add", "origin", bare)

	paths := []string{"dev.yaml", "prod.yaml"}
	for _, p := range paths {
		_ = os.WriteFile(filepath.Join(work, p), []byte(valuesYAML), 0o644)
	}
	mustRun(work, "add", ".")
	mustRun(work, "commit", "-m", "init")
	mustRun(work, "push", "-u", "origin", "main")
	mustRun(work, "remote", "set-head", "origin", "main")

	srv := newFakeGH(t)
	defer srv.Close()

	result, err := RunDirect(DirectOptions{
		Files:         paths,
		Value:         "4.4.4",
		Deploy:        "pr",
		Branch:        "updates/bump",
		Token:         "tok",
		Owner:         "o",
		Repo:          "r",
		GitUserName:   "bot",
		GitUserEmail:  "bot@x",
		WorkDir:       work,
		GitHubBaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("RunDirect: %v", err)
	}
	if len(result.PRURLs) != 1 {
		t.Errorf("PRURLs=%v, want 1", result.PRURLs)
	}
	if srv.prCount.Load() != 1 {
		t.Errorf("want 1 PR, got %d", srv.prCount.Load())
	}
	if len(result.DiffSummary) != 2 {
		t.Errorf("DiffSummary len=%d, want 2", len(result.DiffSummary))
	}
	// Both files on deploy branch.
	for _, p := range paths {
		out, err := exec.Command("git", "-C", bare, "show", "updates/bump:"+p).CombinedOutput()
		if err != nil {
			t.Fatalf("show: %v\n%s", err, out)
		}
		if !strings.Contains(string(out), "tag: 4.4.4") {
			t.Errorf("%s not updated on branch:\n%s", p, out)
		}
	}
}

func TestRunDirect_MultipleFiles_PreflightAtomic(t *testing.T) {
	work, _, _ := directFixture(t, "a.yaml", valuesYAML)
	// Create a second file without an image block.
	bad := filepath.Join(work, "b.yaml")
	if err := os.WriteFile(bad, []byte("foo: bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := RunDirect(DirectOptions{
		Files:        []string{"a.yaml", "b.yaml"},
		Value:        "2.0.0",
		Token:        "tok",
		GitUserName:  "bot",
		GitUserEmail: "bot@x",
		WorkDir:      work,
		Deploy:       "auto",
	})
	if err == nil {
		t.Fatal("want pre-flight error for b.yaml")
	}
	if !strings.Contains(err.Error(), "no target found") {
		t.Errorf("error=%q, want contains 'no target found'", err)
	}
	// a.yaml must be UNCHANGED — atomicity guarantee.
	got, _ := os.ReadFile(filepath.Join(work, "a.yaml"))
	if !strings.Contains(string(got), "tag: 1.0.0") {
		t.Errorf("a.yaml was modified despite pre-flight failure:\n%s", got)
	}
}

func TestRunDirect_MultipleFiles_DryRun(t *testing.T) {
	work, _, _ := directFixture(t, "a.yaml", valuesYAML)
	if err := os.WriteFile(filepath.Join(work, "b.yaml"), []byte(valuesYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := RunDirect(DirectOptions{
		Files:   []string{"a.yaml", "b.yaml"},
		Value:   "9.9.9",
		Token:   "tok",
		WorkDir: work,
		Deploy:  "auto",
		DryRun:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Deployed {
		t.Error("dry-run Deployed should be false")
	}
	if len(result.DiffSummary) != 2 {
		t.Errorf("DiffSummary len=%d, want 2", len(result.DiffSummary))
	}
	// Both files unchanged.
	for _, f := range []string{"a.yaml", "b.yaml"} {
		got, _ := os.ReadFile(filepath.Join(work, f))
		if !strings.Contains(string(got), "tag: 1.0.0") {
			t.Errorf("%s modified in dry-run:\n%s", f, got)
		}
	}
}

func TestRunDirect_AbsolutePath(t *testing.T) {
	work, _, rel := directFixture(t, "values.yaml", valuesYAML)
	abs := filepath.Join(work, rel)

	_, err := RunDirect(DirectOptions{
		Files:        []string{abs}, // absolute path — should work regardless of WorkDir
		Value:        "7.7.7",
		Token:        "tok",
		GitUserName:  "bot",
		GitUserEmail: "bot@x",
		WorkDir:      work,
		Deploy:       "auto",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(abs)
	if !strings.Contains(string(got), "tag: 7.7.7") {
		t.Errorf("not updated:\n%s", got)
	}
}
