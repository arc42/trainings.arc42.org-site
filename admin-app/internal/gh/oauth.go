package gh

import (
	"context"

	"golang.org/x/oauth2"
	ghendpoint "golang.org/x/oauth2/github"
)

type OAuth struct {
	ClientID     string
	ClientSecret string
	Redirect     string
}

func (o OAuth) config() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     o.ClientID,
		ClientSecret: o.ClientSecret,
		RedirectURL:  o.Redirect,
		// public_repo only. The repo is public, so full "repo" scope would
		// grant access to every private repository the user can see.
		Scopes:   []string{"public_repo"},
		Endpoint: ghendpoint.Endpoint,
	}
}

func (o OAuth) AuthURL(state string) string {
	return o.config().AuthCodeURL(state, oauth2.AccessTypeOnline)
}

func (o OAuth) Exchange(ctx context.Context, code string) (string, error) {
	tok, err := o.config().Exchange(ctx, code)
	if err != nil {
		return "", err
	}
	return tok.AccessToken, nil
}
