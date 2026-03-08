package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/houbamzdar/bff/internal/config"
	"golang.org/x/oauth2"
)

type OIDC struct {
	Provider     *oidc.Provider
	OAuth2Config oauth2.Config
	Verifier     *oidc.IDTokenVerifier
}

type tokenEndpointResponse struct {
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresIn        int64  `json:"expires_in"`
	Scope            string `json:"scope"`
	IDToken          string `json:"id_token"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func newOAuth2Config(cfg *config.Config, endpoint oauth2.Endpoint) oauth2.Config {
	// AHOJ420 expects client_secret_basic for confidential clients.
	// Forcing it here avoids oauth2 auth-style probing, which can otherwise
	// trigger a second token request and consume the authorization code.
	endpoint.AuthStyle = oauth2.AuthStyleInHeader

	return oauth2.Config{
		ClientID:     cfg.OIDCClientID,
		ClientSecret: cfg.OIDCClientSecret,
		RedirectURL:  cfg.OIDCRedirectURL,
		Endpoint:     endpoint,
		Scopes:       cfg.OIDCScopes,
	}
}

func NewOIDC(ctx context.Context, cfg *config.Config) (*OIDC, error) {
	provider, err := oidc.NewProvider(ctx, cfg.OIDCIssuer)
	if err != nil {
		return nil, err
	}

	oauth2Config := newOAuth2Config(cfg, provider.Endpoint())

	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.OIDCClientID})

	return &OIDC{
		Provider:     provider,
		OAuth2Config: oauth2Config,
		Verifier:     verifier,
	}, nil
}

func (o *OIDC) ExchangeCode(ctx context.Context, code, verifier string) (*oauth2.Token, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", o.OAuth2Config.RedirectURL)
	form.Set("code_verifier", verifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.OAuth2Config.Endpoint.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create token request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(o.OAuth2Config.ClientID, o.OAuth2Config.ClientSecret)

	client := http.DefaultClient
	if ctxClient, ok := ctx.Value(oauth2.HTTPClient).(*http.Client); ok && ctxClient != nil {
		client = ctxClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}

	var tokenResp tokenEndpointResponse
	if len(body) > 0 {
		if err := json.Unmarshal(body, &tokenResp); err != nil {
			return nil, fmt.Errorf("parse token response: %w", err)
		}
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		if tokenResp.Error != "" {
			if tokenResp.ErrorDescription != "" {
				return nil, fmt.Errorf("%s: %s", tokenResp.Error, tokenResp.ErrorDescription)
			}
			return nil, fmt.Errorf("%s", tokenResp.Error)
		}
		return nil, fmt.Errorf("token endpoint returned %s", resp.Status)
	}

	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("token endpoint returned no access token")
	}

	tokenType := tokenResp.TokenType
	if tokenType == "" {
		tokenType = "Bearer"
	}

	token := &oauth2.Token{
		AccessToken:  tokenResp.AccessToken,
		TokenType:    tokenType,
		RefreshToken: tokenResp.RefreshToken,
	}
	if tokenResp.ExpiresIn > 0 {
		token.Expiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	extra := map[string]interface{}{}
	if tokenResp.IDToken != "" {
		extra["id_token"] = tokenResp.IDToken
	}
	if tokenResp.Scope != "" {
		extra["scope"] = tokenResp.Scope
	}
	if len(extra) > 0 {
		token = token.WithExtra(extra)
	}

	return token, nil
}
