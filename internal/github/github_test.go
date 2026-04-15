package github

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEnsurePR_Creates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "state=open"):
			_ = json.NewEncoder(w).Encode([]PullRequest{})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pulls"):
			_ = json.NewEncoder(w).Encode(PullRequest{
				Number: 42, URL: "https://example.com/pr/42", NodeID: "PR_NODE_1",
			})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/labels"):
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
	}))
	defer srv.Close()

	c := NewClientWithBase(srv.URL, "tok", "owner", "repo")
	pr, err := c.EnsurePR("deploy/svc/dev", "main", "title", "body", []string{"deploy"})
	if err != nil {
		t.Fatal(err)
	}
	if pr.Number != 42 {
		t.Errorf("Number=%d, want 42", pr.Number)
	}
	if pr.NodeID != "PR_NODE_1" {
		t.Errorf("NodeID=%q, want PR_NODE_1", pr.NodeID)
	}
}

func TestEnsurePR_Updates(t *testing.T) {
	var patched bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode([]PullRequest{
				{Number: 7, URL: "https://example.com/pr/7", NodeID: "PR_N7", State: "open"},
			})
		case http.MethodPatch:
			patched = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected: %s %s", r.Method, r.URL)
		}
	}))
	defer srv.Close()

	c := NewClientWithBase(srv.URL, "tok", "owner", "repo")
	pr, err := c.EnsurePR("head", "main", "title", "body", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !patched {
		t.Error("expected PATCH to be called")
	}
	if pr.Number != 7 {
		t.Errorf("Number=%d, want 7", pr.Number)
	}
}

func TestDo_ErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"Validation Failed","errors":[]}`))
	}))
	defer srv.Close()

	c := NewClientWithBase(srv.URL, "tok", "owner", "repo")
	_, err := c.EnsurePR("head", "main", "t", "b", nil)
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "Validation Failed") {
		t.Errorf("error does not include message body: %v", err)
	}
	if !strings.Contains(err.Error(), "422") {
		t.Errorf("error does not include status code: %v", err)
	}
}

func TestEnableAutoMerge_Success(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/graphql" {
			t.Errorf("path=%q, want /graphql", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "enablePullRequestAutoMerge") {
			t.Errorf("mutation missing from body: %s", body)
		}
		if !strings.Contains(string(body), "PR_NODE_1") {
			t.Errorf("nodeID missing from body: %s", body)
		}
		_, _ = w.Write([]byte(`{"data":{"enablePullRequestAutoMerge":{"pullRequest":{"id":"PR_NODE_1"}}}}`))
	}))
	defer srv.Close()

	c := NewClientWithBase(srv.URL, "tok", "owner", "repo")
	if err := c.EnableAutoMerge("PR_NODE_1", "SQUASH"); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("expected graphql call")
	}
}

func TestEnableAutoMerge_GraphQLError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errors":[{"message":"Pull request is in clean status"}]}`))
	}))
	defer srv.Close()

	c := NewClientWithBase(srv.URL, "tok", "owner", "repo")
	err := c.EnableAutoMerge("PR_NODE_1", "SQUASH")
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "clean status") {
		t.Errorf("unexpected: %v", err)
	}
}

func TestEnableAutoMerge_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	defer srv.Close()

	c := NewClientWithBase(srv.URL, "tok", "owner", "repo")
	err := c.EnableAutoMerge("PR_NODE_1", "SQUASH")
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("unexpected: %v", err)
	}
}
