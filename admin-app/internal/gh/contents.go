package gh

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
)

// ReadFile returns a file's contents, its blob SHA, and the current head SHA of
// main. The blob SHA is what makes concurrent-edit detection possible: if it
// has moved by publish time, somebody else changed the file meanwhile.
func (c *Client) ReadFile(ctx context.Context, path string) (content []byte, fileSHA, headSHA string, err error) {
	var file struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
		SHA      string `json:"sha"`
	}
	if err := c.do(ctx, http.MethodGet, c.repoPath("/contents/"+path), nil, &file); err != nil {
		return nil, "", "", err
	}
	if file.Encoding != "base64" {
		return nil, "", "", fmt.Errorf("unexpected content encoding %q", file.Encoding)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(file.Content, "\n", ""))
	if err != nil {
		return nil, "", "", fmt.Errorf("decode contents: %w", err)
	}
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := c.do(ctx, http.MethodGet, c.repoPath("/git/ref/heads/main"), nil, &ref); err != nil {
		return nil, "", "", err
	}
	return decoded, file.SHA, ref.Object.SHA, nil
}
