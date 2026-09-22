// Package muster mocks a muster aggregator on one server: its own OAuth
// authorization server -- the protected-resource metadata of RFC 9728, the
// server metadata of RFC 8414, dynamic client registration, an authorization
// endpoint that redirects straight back with a code the way a browser of a
// person who is signed in already does, the token endpoint issuing access and
// refresh tokens, userinfo -- and the MCP endpoint /mcp over streamable HTTP,
// whose tools answer from the scenario's sequences by tool name.
// DEVCTL_MUSTER_URL is the server's URL plus /mcp.
package muster

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
)

// MCPPath is where the aggregator speaks MCP.
const MCPPath = "/mcp"

// ClientID is the client id the registration endpoint hands out; a client
// id that is an HTTPS URL -- a client ID metadata document, what devctl
// sends -- is accepted as well, without a fetch.
const ClientID = "devctl-e2e-client"

// knownClient says whether the mock serves this client id.
func knownClient(id string) bool {
	return id == ClientID || strings.HasPrefix(id, "https://")
}

// The keys of the OAuth and JSON-RPC answers, and the flow's parameters.
const (
	keyError            = "error"
	keyErrorDescription = "error_description"
	keyMessage          = "message"
	keyContent          = "content"
	keyCode             = "code"
	paramCode           = "code"
	grantRefreshToken   = "refresh_token"
	errInvalidGrant     = "invalid_grant"
)

// ToolResponse is one scripted answer of a tool.
type ToolResponse struct {
	// Args, when given, are arguments the call must carry with these values
	// (compared as JSON); a call that does not is answered with a tool error
	// naming the difference, so the scenario fails visibly.
	Args map[string]any `yaml:"args"`
	// Result is the tool's answer: a mapping or list becomes the structured
	// content (and the JSON text content), a string the text content.
	Result any `yaml:"result"`
	// Error makes the answer a tool error with this text.
	Error string `yaml:"error"`
}

// Config is the mock's script.
type Config struct {
	// Tools maps a tool name, as muster exposes it (x_<server>_<tool>, or
	// core_auth_login), to its sequence of answers: the Nth call gets the
	// Nth answer and the last one repeats.
	Tools map[string][]ToolResponse `yaml:"tools"`
	// Login is the userinfo answer's email; e2e-engineer@example.com when
	// left out.
	Login string `yaml:"login"`
	// Bearer is an access token the MCP endpoint accepts besides the ones it
	// issued: the scenario's keyring token.
	Bearer string `yaml:"bearer"`
	// RefreshToken is a refresh token the token endpoint accepts besides the
	// ones it issued: the scenario's keyring refresh token.
	RefreshToken string `yaml:"refreshToken"`
}

// Server is a running mock.
type Server struct {
	*httptest.Server
	cfg Config

	mu       sync.Mutex
	requests []sequence.Request
	calls    map[string]int
	codes    map[string]string
	access   map[string]bool
	refresh  map[string]bool
	issued   int
}

// Start serves the mock on a loopback port until Close.
func Start(cfg Config) *Server {
	if cfg.Login == "" {
		cfg.Login = "e2e-engineer@example.com"
	}
	s := &Server{
		cfg:     cfg,
		calls:   map[string]int{},
		codes:   map[string]string{},
		access:  map[string]bool{},
		refresh: map[string]bool{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-protected-resource", s.protectedResource)
	mux.HandleFunc("/.well-known/oauth-protected-resource"+MCPPath, s.protectedResource)
	mux.HandleFunc("/.well-known/oauth-authorization-server", s.authorizationServer)
	mux.HandleFunc("/oauth/register", s.register)
	mux.HandleFunc("/oauth/authorize", s.authorize)
	mux.HandleFunc("/oauth/token", s.token)
	mux.HandleFunc("/oauth/userinfo", s.userinfo)
	mux.HandleFunc(MCPPath, s.mcp)
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		mux.ServeHTTP(w, r)
	}))
	return s
}

// MCPURL is the value of DEVCTL_MUSTER_URL.
func (s *Server) MCPURL() string {
	return s.URL + MCPPath
}

// Requests are the requests received so far.
func (s *Server) Requests() []sequence.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]sequence.Request, len(s.requests))
	copy(out, s.requests)
	return out
}

func (s *Server) record(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, sequence.Request{Method: r.Method, Path: r.URL.Path, Query: r.URL.Query(), Header: r.Header.Clone()})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) protectedResource(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 s.MCPURL(),
		"authorization_servers":    []string{s.URL},
		"bearer_methods_supported": []string{"header"},
	})
}

func (s *Server) authorizationServer(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                s.URL,
		"authorization_endpoint":                s.URL + "/oauth/authorize",
		"token_endpoint":                        s.URL + "/oauth/token",
		"registration_endpoint":                 s.URL + "/oauth/register",
		"userinfo_endpoint":                     s.URL + "/oauth/userinfo",
		"response_types_supported":              []string{paramCode},
		"grant_types_supported":                 []string{"authorization_code", grantRefreshToken},
		"code_challenge_methods_supported":      []string{"S256"},
		"client_id_metadata_document_supported": true,
		"token_endpoint_auth_methods_supported": []string{"none"},
	})
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{keyError: "invalid_request"})
		return
	}
	var reg struct {
		RedirectURIs []string `json:"redirect_uris"`
	}
	if err := json.NewDecoder(r.Body).Decode(&reg); err != nil || len(reg.RedirectURIs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{keyError: "invalid_client_metadata"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"client_id": ClientID, "redirect_uris": reg.RedirectURIs})
}

// authorize is the person's browser session: signed in already, it redirects
// straight back to the client with a fresh code bound to the PKCE challenge.
func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	redirect, err := url.Parse(q.Get("redirect_uri"))
	if err != nil || redirect.Host == "" || !isLoopback(redirect.Hostname()) {
		http.Error(w, "redirect_uri missing or not a loopback address", http.StatusBadRequest)
		return
	}
	if q.Get("response_type") != paramCode || !knownClient(q.Get("client_id")) || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("state") == "" {
		back := redirect.Query()
		back.Set(keyError, "invalid_request")
		back.Set("state", q.Get("state"))
		redirect.RawQuery = back.Encode()
		http.Redirect(w, r, redirect.String(), http.StatusFound) //nolint:gosec // G710: an authorization server redirects to the client's redirect_uri; this test double accepts loopback ones
		return
	}
	s.mu.Lock()
	s.issued++
	code := fmt.Sprintf("code-%d", s.issued)
	s.codes[code] = q.Get("code_challenge")
	s.mu.Unlock()
	back := redirect.Query()
	back.Set(paramCode, code)
	back.Set("state", q.Get("state"))
	back.Set("iss", s.URL)
	redirect.RawQuery = back.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound) //nolint:gosec // G710: an authorization server redirects to the client's redirect_uri; this test double accepts loopback ones
}

// isLoopback says whether host is the loopback the client listens on.
func isLoopback(host string) bool {
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{keyError: "invalid_request"})
		return
	}
	if !knownClient(r.Form.Get("client_id")) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{keyError: "invalid_client"})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.Form.Get("grant_type") {
	case "authorization_code":
		challenge, ok := s.codes[r.Form.Get(paramCode)]
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]any{keyError: errInvalidGrant, keyErrorDescription: "unknown code"})
			return
		}
		delete(s.codes, r.Form.Get(paramCode))
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
			writeJSON(w, http.StatusBadRequest, map[string]any{keyError: errInvalidGrant, keyErrorDescription: "PKCE verifier does not match"})
			return
		}
	case grantRefreshToken:
		token := r.Form.Get(grantRefreshToken)
		if !s.refresh[token] && (s.cfg.RefreshToken == "" || token != s.cfg.RefreshToken) {
			writeJSON(w, http.StatusBadRequest, map[string]any{keyError: errInvalidGrant, keyErrorDescription: "unknown refresh token"})
			return
		}
		delete(s.refresh, token)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{keyError: "unsupported_grant_type"})
		return
	}
	s.issued++
	access := fmt.Sprintf("muster-access-%d", s.issued)
	refresh := fmt.Sprintf("muster-refresh-%d", s.issued)
	s.access[access] = true
	s.refresh[refresh] = true
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": access, "token_type": "Bearer", "expires_in": 3600,
		grantRefreshToken: refresh, "scope": "openid profile email groups offline_access",
	})
}

// accepts says whether the bearer of r is one the mock issued or the
// scenario's.
func (s *Server) accepts(r *http.Request) bool {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" || token == r.Header.Get("Authorization") {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.access[token] || (s.cfg.Bearer != "" && token == s.cfg.Bearer)
}

func (s *Server) unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata="%s/.well-known/oauth-protected-resource", error="invalid_token"`, s.URL))
	writeJSON(w, http.StatusUnauthorized, map[string]any{keyError: "invalid_token", keyErrorDescription: "Missing or invalid Authorization header"})
}

func (s *Server) userinfo(w http.ResponseWriter, r *http.Request) {
	if !s.accepts(r) {
		s.unauthorized(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sub": "e2e-sub", "email": s.cfg.Login})
}

// rpc is a JSON-RPC message, request or response.
type rpc struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"params"`
}

func (s *Server) mcp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !s.accepts(r) {
		s.unauthorized(w)
		return
	}
	var msg rpc
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"jsonrpc": "2.0", keyError: map[string]any{keyCode: -32700, keyMessage: "parse error"}})
		return
	}
	w.Header().Set("Mcp-Session-Id", "muster-e2e-session")
	respond := func(result any, rpcErr map[string]any) {
		body := map[string]any{"jsonrpc": "2.0", "id": msg.ID}
		if rpcErr != nil {
			body[keyError] = rpcErr
		} else {
			body["result"] = result
		}
		writeJSON(w, http.StatusOK, body)
	}
	switch msg.Method {
	case "initialize":
		respond(map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "muster", "version": "e2e"},
		}, nil)
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "tools/call":
		answer, ok := s.next(msg.Params.Name)
		if !ok {
			respond(nil, map[string]any{keyCode: -32602, keyMessage: fmt.Sprintf("tool %s not found", msg.Params.Name)})
			return
		}
		if diff := unexpectedArgs(answer.Args, msg.Params.Arguments); diff != "" {
			respond(toolResult(ToolResponse{Error: fmt.Sprintf("unexpected arguments for %s: %s", msg.Params.Name, diff)}), nil)
			return
		}
		respond(toolResult(answer), nil)
	default:
		respond(nil, map[string]any{keyCode: -32601, keyMessage: fmt.Sprintf("method %s not found", msg.Method)})
	}
}

// unexpectedArgs names the first expected argument the call does not carry
// with the expected value, both sides normalised through JSON; empty when
// every expected argument matches.
func unexpectedArgs(want, got map[string]any) string {
	keys := make([]string, 0, len(want))
	for k := range want {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		w, _ := json.Marshal(normalise(want[k]))
		g, _ := json.Marshal(normalise(got[k]))
		if string(w) != string(g) {
			return fmt.Sprintf("%s = %s, want %s", k, g, w)
		}
	}
	return ""
}

// normalise round-trips a value through JSON so YAML's and the client's
// number and map types compare equal.
func normalise(v any) any {
	data, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	_ = json.Unmarshal(data, &out)
	return out
}

// next is the tool's next scripted answer.
func (s *Server) next(tool string) (ToolResponse, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	answers := s.cfg.Tools[tool]
	if len(answers) == 0 {
		return ToolResponse{}, false
	}
	n := s.calls[tool]
	s.calls[tool]++
	if n >= len(answers) {
		n = len(answers) - 1
	}
	return answers[n], true
}

// toolResult renders a scripted answer as an MCP call result.
func toolResult(answer ToolResponse) map[string]any {
	if answer.Error != "" {
		return map[string]any{"isError": true, keyContent: textContent(answer.Error)}
	}
	if text, ok := answer.Result.(string); ok {
		return map[string]any{keyContent: textContent(text)}
	}
	data, err := json.Marshal(answer.Result)
	if err != nil {
		return map[string]any{"isError": true, keyContent: textContent("scripted result is not JSON: " + err.Error())}
	}
	return map[string]any{
		"structuredContent": json.RawMessage(data),
		keyContent:          textContent(string(data)),
	}
}

// textContent is one text content block.
func textContent(text string) []map[string]any {
	return []map[string]any{{"type": "text", "text": text}}
}
