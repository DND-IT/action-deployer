// Package github wraps GitHub REST API calls needed for PR-based deploys.
package github

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

// Client is a minimal GitHub REST client.
type Client struct {
	Token  string
	Owner  string
	Repo   string
	client *http.Client
}

// NewClient creates a Client for the given owner/repo.
func NewClient(token, owner, repo string) *Client {
	return &Client{
		Token:  token,
		Owner:  owner,
		Repo:   repo,
		client: &http.Client{},
	}
}

// PullRequest represents a GitHub pull request.
type PullRequest struct {
	Number int    `json:"number"`
	URL    string `json:"html_url"`
	State  string `json:"state"`
}

// EnsurePR creates a PR from head → base, or finds the existing open PR.
// Returns the PR URL.
func (c *Client) EnsurePR(head, base, title, body string, labels []string) (string, error) {
	existing, err := c.findOpenPR(head, base)
	if err != nil {
		return "", err
	}
	if existing != nil {
		return existing.URL, c.updatePR(existing.Number, title, body)
	}
	pr, err := c.createPR(head, base, title, body)
	if err != nil {
		return "", err
	}
	if len(labels) > 0 {
		_ = c.addLabels(pr.Number, labels) // best-effort
	}
	return pr.URL, nil
}

func (c *Client) findOpenPR(head, base string) (*PullRequest, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls?state=open&head=%s:%s&base=%s",
		c.Owner, c.Repo, c.Owner, head, base)
	var prs []PullRequest
	if err := c.get(url, &prs); err != nil {
		return nil, err
	}
	if len(prs) == 0 {
		return nil, nil
	}
	return &prs[0], nil
}

func (c *Client) createPR(head, base, title, body string) (*PullRequest, error) {
	payload := map[string]string{"title": title, "head": head, "base": base, "body": body}
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls", c.Owner, c.Repo)
	var pr PullRequest
	return &pr, c.post(url, payload, &pr)
}

func (c *Client) updatePR(number int, title, body string) error {
	payload := map[string]string{"title": title, "body": body}
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d", c.Owner, c.Repo, number)
	return c.patch(url, payload, nil)
}

func (c *Client) addLabels(number int, labels []string) error {
	payload := map[string][]string{"labels": labels}
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%d/labels", c.Owner, c.Repo, number)
	return c.post(url, payload, nil)
}

func (c *Client) get(url string, out any) error {
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	return c.do(req, out)
}

func (c *Client) post(url string, body, out any) error {
	return c.doJSON(http.MethodPost, url, body, out)
}

func (c *Client) patch(url string, body, out any) error {
	return c.doJSON(http.MethodPatch, url, body, out)
}

func (c *Client) doJSON(method, url string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, _ := http.NewRequest(method, url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("http %s %s: %w", req.Method, req.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("http %s %s: status %d", req.Method, req.URL, resp.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
