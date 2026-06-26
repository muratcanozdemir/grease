package enrich

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/user/grease/internal/types"
)

// EnvAPIKey is the environment variable grease reads the Hunter key from.
//
// The key is read directly from the process environment — no dotenv, no config
// file, no flag that would land it in shell history. BYOK means the user
// exports it in their shell; grease never persists it anywhere.
const EnvAPIKey = "HUNTER_API_KEY"

// hunterBaseURL is the Domain Search endpoint. Overridable in tests.
const hunterBaseURL = "https://api.hunter.io/v2/domain-search"

// HunterProvider is an EmailProvider backed by Hunter's Domain Search API.
//
// Scope is deliberately one endpoint: Domain Search returns names, positions,
// departments, and emails for a domain in a single call, which is exactly
// grease's "who works here" question. Email Finder (name -> address) is a
// different question grease does not ask, so it is not wired.
//
// grease makes ONE call per run; it is not a bulk processor. That shapes the
// error handling: a 429 from a single call is an account-level rate limit, and
// the correct response is to tell the user, not to retry. Retrying inside the
// tool is the un-throttled-retry pattern Hunter explicitly warns leads to key
// suspension — and it is the agent-loop pattern grease rejects on principle. So
// every failure here is surfaced, not swallowed or worked around.
type HunterProvider struct {
	APIKey  string
	BaseURL string
	HTTP    *http.Client
}

// NewHunterFromEnv constructs a provider with the key from the environment.
// Returns an error if the key is absent, so the failure is a clear "you didn't
// set HUNTER_API_KEY" rather than a confusing 401 later.
func NewHunterFromEnv() (*HunterProvider, error) {
	key := strings.TrimSpace(os.Getenv(EnvAPIKey))
	if key == "" {
		return nil, fmt.Errorf("enrich: %s is not set in the environment; export your Hunter API key (grease never stores it)", EnvAPIKey)
	}
	return &HunterProvider{
		APIKey:  key,
		BaseURL: hunterBaseURL,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// hunterResponse mirrors the Domain Search JSON. Only the fields grease uses are
// declared; Hunter returns more (sources, etc.) which are ignored.
type hunterResponse struct {
	Data struct {
		Domain       string        `json:"domain"`
		Organization string        `json:"organization"`
		Pattern      string        `json:"pattern"`
		Emails       []hunterEmail `json:"emails"`
	} `json:"data"`
	Errors []hunterError `json:"errors"`
}

type hunterEmail struct {
	Value      string `json:"value"`
	Type       string `json:"type"` // "personal" | "generic"
	Confidence int    `json:"confidence"`
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
	Position   string `json:"position"`
	Seniority  string `json:"seniority"`
	Department string `json:"department"`
}

type hunterError struct {
	ID      string `json:"id"`
	Code    int    `json:"code"`
	Details string `json:"details"`
}

// FindByDomain implements EmailProvider against Hunter Domain Search.
func (h *HunterProvider) FindByDomain(ctx context.Context, domain string) (Result, error) {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return Result{}, fmt.Errorf("enrich: empty domain")
	}
	if h.APIKey == "" {
		return Result{}, fmt.Errorf("enrich: no API key configured")
	}
	base := h.BaseURL
	if base == "" {
		base = hunterBaseURL
	}

	q := url.Values{}
	q.Set("domain", domain)
	q.Set("api_key", h.APIKey)
	reqURL := base + "?" + q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return Result{}, fmt.Errorf("enrich: build request: %w", err)
	}

	client := h.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return Result{}, fmt.Errorf("enrich: request to Hunter: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return Result{}, fmt.Errorf("enrich: read response: %w", err)
	}

	// Specific, actionable messages for the failures a user will actually hit,
	// before generic status handling. These are the ones worth naming because
	// the fix differs: a 401 is "your key is wrong", a 429 is "you are rate
	// limited, wait" — not the same remedy.
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return Result{}, fmt.Errorf("enrich: Hunter rejected the API key (401); check that %s holds a valid key — %s", EnvAPIKey, hunterDetail(raw))
	case http.StatusTooManyRequests:
		// One call hit the rate limit: an account-level throttle. grease does
		// not retry (single-shot tool, and retrying is the suspension-risk
		// antipattern). Surface it and let the user decide.
		return Result{}, fmt.Errorf("enrich: Hunter rate limit hit (429); the key is throttled at the account level — wait before retrying (grease does not auto-retry)")
	}

	var parsed hunterResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Result{}, fmt.Errorf("enrich: status %d, unparseable Hunter response: %s", resp.StatusCode, snippet(raw))
	}

	// Hunter reports application errors in the errors array even on some
	// non-2xx codes; treat any populated errors array as failure with Hunter's
	// own message.
	if len(parsed.Errors) > 0 {
		e := parsed.Errors[0]
		return Result{}, fmt.Errorf("enrich: Hunter error (%s, code %d): %s", e.ID, e.Code, e.Details)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("enrich: Hunter returned status %d: %s", resp.StatusCode, snippet(raw))
	}

	return toResult(parsed), nil
}

// toResult maps Hunter's wire shape onto grease's domain types. Department
// strings are passed through as types.Department; an unrecognized one is left
// as-is here (not coerced) and the downstream filter simply won't match it —
// the filter treats only known departments as matches.
func toResult(r hunterResponse) Result {
	contacts := make([]types.Contact, 0, len(r.Data.Emails))
	for _, e := range r.Data.Emails {
		contacts = append(contacts, types.Contact{
			Email:      e.Value,
			FirstName:  e.FirstName,
			LastName:   e.LastName,
			Position:   e.Position,
			Seniority:  e.Seniority,
			Department: types.Department(e.Department),
			Confidence: e.Confidence,
		})
	}
	return Result{
		Organization: types.Organization{
			Name:    r.Data.Organization,
			Domain:  r.Data.Domain,
			Pattern: r.Data.Pattern,
		},
		Contacts: contacts,
	}
}

func hunterDetail(raw []byte) string {
	var parsed hunterResponse
	if json.Unmarshal(raw, &parsed) == nil && len(parsed.Errors) > 0 {
		return parsed.Errors[0].Details
	}
	return ""
}

func snippet(b []byte) string {
	const max = 300
	s := strings.TrimSpace(string(b))
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
