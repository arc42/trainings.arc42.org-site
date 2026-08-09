// Package gh talks to the GitHub REST API: sign-in, permission check, reading
// the trainings file, and opening a pull request.
package gh

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	BaseURL string
	Owner   string
	Repo    string
	Token   string
	HTTP    *http.Client
}

// New builds a client. baseURL is "https://api.github.com" in production and an
// httptest server URL in tests.
func New(baseURL, owner, repo, token string) *Client {
	return &Client{
		BaseURL: baseURL,
		Owner:   owner,
		Repo:    repo,
		Token:   token,
		HTTP:    &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("github %s %s: %s: %s", method, path, resp.Status, bytes.TrimSpace(msg))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) repoPath(suffix string) string {
	return fmt.Sprintf("/repos/%s/%s%s", c.Owner, c.Repo, suffix)
}

// Viewer returns the signed-in login and whether they may push to the repo.
// Authorization is exactly this check — there is deliberately no allowlist in
// the app, so access is whatever GitHub currently says it is.
func (c *Client) Viewer(ctx context.Context) (string, bool, error) {
	var user struct {
		Login string `json:"login"`
	}
	if err := c.do(ctx, http.MethodGet, "/user", nil, &user); err != nil {
		return "", false, err
	}
	var repo struct {
		Permissions struct {
			Push bool `json:"push"`
		} `json:"permissions"`
	}
	if err := c.do(ctx, http.MethodGet, c.repoPath(""), nil, &repo); err != nil {
		return user.Login, false, err
	}
	return user.Login, repo.Permissions.Push, nil
}
