package gh

import (
	"context"
	"encoding/base64"
	"net/http"
)

type PRRequest struct {
	Branch  string
	Path    string
	Content string
	BaseSHA string // head of main when the branch is cut
	FileSHA string // blob SHA of the file being replaced
	Title   string
	Body    string
}

// OpenPR creates a branch, commits the new file content onto it, and opens a
// pull request against main. It never writes to main directly — merging stays a
// human maintainer's action, which is the app's primary security boundary.
func (c *Client) OpenPR(ctx context.Context, req PRRequest) (string, error) {
	if err := c.do(ctx, http.MethodPost, c.repoPath("/git/refs"), map[string]string{
		"ref": "refs/heads/" + req.Branch,
		"sha": req.BaseSHA,
	}, nil); err != nil {
		return "", err
	}
	if err := c.do(ctx, http.MethodPut, c.repoPath("/contents/"+req.Path), map[string]string{
		"message": req.Title,
		"content": base64.StdEncoding.EncodeToString([]byte(req.Content)),
		"sha":     req.FileSHA,
		"branch":  req.Branch,
	}, nil); err != nil {
		return "", err
	}
	var pr struct {
		HTMLURL string `json:"html_url"`
	}
	if err := c.do(ctx, http.MethodPost, c.repoPath("/pulls"), map[string]string{
		"title": req.Title,
		"body":  req.Body,
		"head":  req.Branch,
		"base":  "main",
	}, &pr); err != nil {
		return "", err
	}
	return pr.HTMLURL, nil
}
