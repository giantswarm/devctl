package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Caller is the person a call is made for, as the ID token the gateway in
// front forwards names them (muster's forwardToken).
type Caller struct {
	// Subject is the token's sub, the person's stable identity at the
	// issuer.
	Subject string
	// Email is the token's email claim, when it carries one.
	Email string
}

// Verifier checks the bearer token of a call and names its caller.
type Verifier interface {
	Verify(ctx context.Context, rawIDToken string) (Caller, error)
}

// OIDCVerifier accepts an ID token its issuer signed for one of its
// audiences.
type OIDCVerifier struct {
	verifier  *oidc.IDTokenVerifier
	audiences []string
}

// NewOIDCVerifier discovers issuer's signing keys and accepts its ID tokens
// for any of audiences.
func NewOIDCVerifier(ctx context.Context, issuer string, audiences []string) (*OIDCVerifier, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("discovering the issuer %s: %w", issuer, err)
	}
	return newOIDCVerifier(provider.VerifierContext(ctx, &oidc.Config{SkipClientIDCheck: true}), audiences)
}

// NewStaticOIDCVerifier accepts issuer's ID tokens signed by keys for any of
// audiences, with no discovery.
func NewStaticOIDCVerifier(issuer string, keys oidc.KeySet, audiences []string) (*OIDCVerifier, error) {
	return newOIDCVerifier(oidc.NewVerifier(issuer, keys, &oidc.Config{SkipClientIDCheck: true}), audiences)
}

func newOIDCVerifier(verifier *oidc.IDTokenVerifier, audiences []string) (*OIDCVerifier, error) {
	if len(audiences) == 0 {
		return nil, errors.New("an ID token verifier needs at least one trusted audience")
	}
	return &OIDCVerifier{verifier: verifier, audiences: audiences}, nil
}

// Verify implements [Verifier].
func (v *OIDCVerifier) Verify(ctx context.Context, rawIDToken string) (Caller, error) {
	token, err := v.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return Caller{}, err
	}
	if !slices.ContainsFunc(token.Audience, func(aud string) bool { return slices.Contains(v.audiences, aud) }) {
		return Caller{}, fmt.Errorf("the token's audiences %v hold none this server trusts", token.Audience)
	}
	var claims struct {
		Email string `json:"email"`
	}
	if err := token.Claims(&claims); err != nil {
		return Caller{}, err
	}
	if token.Subject == "" {
		return Caller{}, errors.New("the token names no subject")
	}
	return Caller{Subject: token.Subject, Email: claims.Email}, nil
}

type callerKey struct{}

// CallerFrom is the caller of the call in ctx, if the server verified one.
func CallerFrom(ctx context.Context) (Caller, bool) {
	c, ok := ctx.Value(callerKey{}).(Caller)
	return c, ok
}

// authenticate refuses a request without a bearer token verifier accepts,
// and puts the caller it names into the request's context.
func authenticate(verifier Verifier, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, raw, _ := strings.Cut(r.Header.Get("Authorization"), " ")
		if !strings.EqualFold(scheme, "Bearer") || raw == "" {
			unauthorized(w, "a bearer ID token is required")
			return
		}
		caller, err := verifier.Verify(r.Context(), raw)
		if err != nil {
			unauthorized(w, "the bearer ID token is not accepted")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerKey{}, caller)))
	})
}

func unauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
	http.Error(w, message, http.StatusUnauthorized)
}
