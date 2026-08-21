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
	// Endpoint is where the browser is sent to authorize and where the code is
	// traded for a token. Zero means github.com. It is configurable so the app
	// can run its real sign-in flow against a stand-in — see EndpointFor.
	Endpoint oauth2.Endpoint
}

// EndpointFor returns the OAuth endpoints that belong with a given REST API
// base. Production is api.github.com, whose sign-in lives on github.com.
// Anything else is a stand-in — the offline demo's fake, or an httptest server
// — and serves its own sign-in on the same host.
//
// This is what keeps the demo honest: it runs the same handlers, the same
// state-cookie check and the same code-for-token exchange as production. There
// is no "if demo, skip sign-in" branch anywhere in the app, so there is no
// bypass that could ever be reached with a real configuration.
func EndpointFor(apiBase string) oauth2.Endpoint {
	if apiBase == "" || apiBase == "https://api.github.com" {
		return ghendpoint.Endpoint
	}
	return oauth2.Endpoint{
		AuthURL:  apiBase + "/login/oauth/authorize",
		TokenURL: apiBase + "/login/oauth/access_token",
	}
}

func (o OAuth) config() *oauth2.Config {
	endpoint := o.Endpoint
	if endpoint.AuthURL == "" || endpoint.TokenURL == "" {
		endpoint = ghendpoint.Endpoint
	}
	return &oauth2.Config{
		ClientID:     o.ClientID,
		ClientSecret: o.ClientSecret,
		RedirectURL:  o.Redirect,
		// public_repo only. The repo is public, so full "repo" scope would
		// grant access to every private repository the user can see.
		Scopes:   []string{"public_repo"},
		Endpoint: endpoint,
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
