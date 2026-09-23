package authstore

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"
)

// GitHubAppClientID is the client id of the giantswarm-devctl GitHub App,
// owned by the giantswarm organization. The device flow and the refresh need
// nothing else: an App used through the device flow refreshes without a
// client secret, and a client id is public, so it is embedded here.
const GitHubAppClientID = "Iv23liWio5REm4MfY2Mw"

// GitHub's device-flow endpoints under the OAuth host and the grant types.
const (
	githubDeviceCodePath = "/login/device/code"
	githubAccessPath     = "/login/oauth/access_token"
	githubUserPath       = "/user"

	grantTypeDeviceCode = "urn:ietf:params:oauth:grant-type:device_code"
	grantTypeRefresh    = "refresh_token"

	// devicePollInterval is used when the response names none.
	devicePollInterval = 5 * time.Second
	// slowDownIncrement is what a slow_down answer adds to the interval.
	slowDownIncrement = 5 * time.Second
)

// deviceCode is GitHub's answer to the device-code request.
type deviceCode struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int64  `json:"expires_in"`
	Interval        int64  `json:"interval"`
	Error           string `json:"error"`
}

func (d *deviceCode) oauthError() string { return d.Error }

type githubUser struct {
	Login string `json:"login"`
}

// LoginGitHub runs the device flow: prints the verification URL and the
// user code to stderr, opens the browser, polls until the human has
// authorized, reads the login and stores the record. The token never
// leaves the store.
func (a *Auth) LoginGitHub(ctx context.Context) (Identity, error) {
	var dc deviceCode
	err := a.postForm(ctx, a.endpoints.GitHubOAuthURL+githubDeviceCodePath, url.Values{paramClientID: {GitHubAppClientID}}, &dc)
	if err != nil {
		return Identity{}, fmt.Errorf("requesting a GitHub device code: %w", err)
	}
	if dc.Error != "" {
		return Identity{}, fmt.Errorf("requesting a GitHub device code: %s", dc.Error)
	}
	if dc.DeviceCode == "" || dc.UserCode == "" || dc.VerificationURI == "" {
		return Identity{}, errors.New("requesting a GitHub device code: incomplete response")
	}

	fmt.Fprintf(a.stderr, "GitHub: open %s and enter the code %s\n", dc.VerificationURI, dc.UserCode)
	a.browse(dc.VerificationURI)

	token, err := a.pollDeviceToken(ctx, dc)
	if err != nil {
		return Identity{}, err
	}
	login, err := a.githubLogin(ctx, token.AccessToken)
	if err != nil {
		return Identity{}, err
	}
	now := a.clock.Now()
	record := githubRecord(token, now)
	record.Login = login
	if err := a.store.Set(UserGitHub, record); err != nil {
		return Identity{}, err
	}
	return describe(UserGitHub, record, now), nil
}

// pollDeviceToken asks for the token at the interval GitHub named until the
// human has authorized, the code expires or ctx ends.
func (a *Auth) pollDeviceToken(ctx context.Context, dc deviceCode) (*oauthToken, error) {
	interval := devicePollInterval
	if dc.Interval > 0 {
		interval = time.Duration(dc.Interval) * time.Second
	}
	if dc.ExpiresIn > 0 {
		var cancel context.CancelFunc
		ctx, cancel = a.clock.Timeout(ctx, time.Duration(dc.ExpiresIn)*time.Second)
		defer cancel()
	}
	form := url.Values{
		paramClientID:  {GitHubAppClientID},
		"device_code":  {dc.DeviceCode},
		paramGrantType: {grantTypeDeviceCode},
	}
	for {
		if err := a.clock.Sleep(ctx, interval); err != nil {
			return nil, fmt.Errorf("GitHub authorization not completed in time: %w", err)
		}
		var token oauthToken
		if err := a.postForm(ctx, a.endpoints.GitHubOAuthURL+githubAccessPath, form, &token); err != nil {
			return nil, fmt.Errorf("polling GitHub for the token: %w", err)
		}
		switch token.Error {
		case "":
			if token.AccessToken == "" {
				return nil, errors.New("polling GitHub for the token: empty token")
			}
			return &token, nil
		case "authorization_pending":
		case "slow_down":
			interval += slowDownIncrement
		case "expired_token":
			return nil, errors.New("the GitHub device code expired before the authorization; run `devctl auth login` again")
		case "access_denied":
			return nil, errors.New("the GitHub authorization was denied in the browser")
		default:
			return nil, fmt.Errorf("polling GitHub for the token: %s", token.Error)
		}
	}
}

// refreshGitHub trades the refresh token for a new pair and stores it; the
// old pair stops working on GitHub's side the moment the new one is issued.
func (a *Auth) refreshGitHub(ctx context.Context, record Record) (Record, error) {
	form := url.Values{
		paramClientID:   {GitHubAppClientID},
		paramGrantType:  {grantTypeRefresh},
		"refresh_token": {record.RefreshToken},
	}
	var token oauthToken
	if err := a.postForm(ctx, a.endpoints.GitHubOAuthURL+githubAccessPath, form, &token); err != nil {
		return Record{}, fmt.Errorf("refreshing the GitHub token: %w", err)
	}
	if token.Error != "" || token.AccessToken == "" {
		cause := "the token expired and GitHub refused the refresh"
		if token.Error != "" {
			cause += " (" + token.Error + ")"
		}
		return Record{}, &AuthRequiredError{Identity: identityGitHub, Cause: cause, Hint: hintLoginGitHub}
	}
	fresh := githubRecord(&token, a.clock.Now())
	fresh.Login = record.Login
	if err := a.store.Set(UserGitHub, fresh); err != nil {
		return Record{}, err
	}
	return fresh, nil
}

func githubRecord(token *oauthToken, now time.Time) Record {
	record := Record{Token: token.AccessToken, RefreshToken: token.RefreshToken}
	if token.ExpiresIn > 0 {
		record.ExpiresAt = now.Add(time.Duration(token.ExpiresIn) * time.Second).UTC()
	}
	if token.RefreshToken != "" && token.RefreshTokenExpiresIn > 0 {
		record.RefreshExpiresAt = now.Add(time.Duration(token.RefreshTokenExpiresIn) * time.Second).UTC()
	}
	return record
}

func (a *Auth) githubLogin(ctx context.Context, token string) (string, error) {
	var user githubUser
	headers := map[string]string{"Authorization": "Bearer " + token}
	if err := a.getJSON(ctx, a.endpoints.GitHubAPIURL+githubUserPath, headers, &user); err != nil {
		return "", fmt.Errorf("reading the GitHub login: %w", err)
	}
	if user.Login == "" {
		return "", errors.New("reading the GitHub login: empty login")
	}
	return user.Login, nil
}

// RequireGitHub is the GitHub token: the stored one while it is valid, a
// refreshed one when it expired and the refresh token has not, and
// [ErrAuthRequired] otherwise.
func (a *Auth) RequireGitHub(ctx context.Context) (Token, error) {
	return a.requireGitHub(ctx, hintLogin)
}

// requireGitHub is [Auth.RequireGitHub] with the login a missing record
// hints at: the agent-facing commands need CircleCI too, the others only
// GitHub.
func (a *Auth) requireGitHub(ctx context.Context, hintMissing string) (Token, error) {
	record, err := a.store.Get(UserGitHub)
	if errors.Is(err, ErrNotFound) {
		return Token{}, &AuthRequiredError{Identity: identityGitHub, Cause: causeNoToken, Hint: hintMissing}
	}
	if err != nil {
		return Token{}, err
	}
	now := a.clock.Now()
	if expired(record.ExpiresAt, now) {
		if record.RefreshToken == "" || expired(record.RefreshExpiresAt, now) {
			return Token{}, &AuthRequiredError{Identity: identityGitHub, Cause: "the token expired and the refresh token with it", Hint: hintLoginGitHub}
		}
		record, err = a.refreshGitHub(ctx, record)
		if err != nil {
			return Token{}, err
		}
	}
	return Token{Value: record.Token, Login: record.Login, ExpiresAt: record.ExpiresAt, Source: SourceKeychain}, nil
}
