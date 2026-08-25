package enrich

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/user/grease/internal/types"
)

// hunterServer returns a test server replying with the given status and body,
// and records the query the adapter sent.
func hunterServer(t *testing.T, status int, body string, capturedQuery *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capturedQuery != nil {
			*capturedQuery = r.URL.RawQuery
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
}

const validHunterBody = `{
  "data": {
    "domain": "acme.com",
    "organization": "Acme Payments",
    "pattern": "{first}.{last}",
    "emails": [
      {"value":"ada.lovelace@acme.com","type":"personal","confidence":94,"first_name":"Ada","last_name":"Lovelace","position":"VP Engineering","seniority":"executive","department":"it"},
      {"value":"info@acme.com","type":"generic","confidence":70,"first_name":null,"last_name":null,"position":null,"seniority":null,"department":null}
    ]
  },
  "meta": {"results": 2}
}`

func TestFindByDomain_ParsesContacts(t *testing.T) {
	var query string
	srv := hunterServer(t, http.StatusOK, validHunterBody, &query)
	defer srv.Close()

	h := &HunterProvider{APIKey: "k", BaseURL: srv.URL, HTTP: srv.Client()}
	res, err := h.FindByDomain(context.Background(), "acme.com")
	if err != nil {
		t.Fatalf("FindByDomain: %v", err)
	}

	// Query must carry the domain and the key.
	if !strings.Contains(query, "domain=acme.com") {
		t.Errorf("query missing domain: %s", query)
	}
	if !strings.Contains(query, "api_key=k") {
		t.Errorf("query missing api_key: %s", query)
	}

	if res.Organization.Name != "Acme Payments" {
		t.Errorf("org name = %q", res.Organization.Name)
	}
	if res.Organization.Pattern != "{first}.{last}" {
		t.Errorf("pattern = %q", res.Organization.Pattern)
	}
	if len(res.Contacts) != 2 {
		t.Fatalf("got %d contacts, want 2", len(res.Contacts))
	}
	c0 := res.Contacts[0]
	if c0.Email != "ada.lovelace@acme.com" || c0.FirstName != "Ada" || c0.Department != types.DeptIT || c0.Confidence != 94 {
		t.Errorf("contact[0] mapped wrong: %+v", c0)
	}
	// Null JSON fields must decode to zero values, not crash.
	c1 := res.Contacts[1]
	if c1.Email != "info@acme.com" || c1.FirstName != "" || c1.Department != types.DeptUnknown && c1.Department != "" {
		t.Errorf("contact[1] mapped wrong: %+v", c1)
	}
}

func TestFindByDomain_EmptyEmailsIsNotError(t *testing.T) {
	body := `{"data":{"domain":"empty.com","organization":"Empty","pattern":null,"emails":[]},"meta":{"results":0}}`
	srv := hunterServer(t, http.StatusOK, body, nil)
	defer srv.Close()

	h := &HunterProvider{APIKey: "k", BaseURL: srv.URL, HTTP: srv.Client()}
	res, err := h.FindByDomain(context.Background(), "empty.com")
	if err != nil {
		t.Fatalf("empty result should not be an error: %v", err)
	}
	if len(res.Contacts) != 0 {
		t.Errorf("expected 0 contacts, got %d", len(res.Contacts))
	}
}

func TestFindByDomain_401IsActionable(t *testing.T) {
	body := `{"errors":[{"id":"authentication_failed","code":401,"details":"No user found for the API key supplied"}]}`
	srv := hunterServer(t, http.StatusUnauthorized, body, nil)
	defer srv.Close()

	h := &HunterProvider{APIKey: "bad", BaseURL: srv.URL, HTTP: srv.Client()}
	_, err := h.FindByDomain(context.Background(), "acme.com")
	if err == nil {
		t.Fatal("expected 401 error")
	}
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), EnvAPIKey) {
		t.Errorf("401 error should name the env var and code, got: %v", err)
	}
}

func TestFindByDomain_429DoesNotRetryAndExplains(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"errors":[{"id":"too_many_requests","code":429,"details":"rate limited"}]}`)
	}))
	defer srv.Close()

	h := &HunterProvider{APIKey: "k", BaseURL: srv.URL, HTTP: srv.Client()}
	_, err := h.FindByDomain(context.Background(), "acme.com")
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Errorf("expected 429 error, got: %v", err)
	}
	// The whole point: exactly one call, no retry loop.
	if calls != 1 {
		t.Errorf("adapter retried on 429 (made %d calls); it must make exactly 1", calls)
	}
	if !strings.Contains(err.Error(), "does not auto-retry") {
		t.Errorf("429 error should explain no-retry policy, got: %v", err)
	}
}

func TestFindByDomain_ErrorArraySurfaced(t *testing.T) {
	body := `{"errors":[{"id":"wrong_params","code":400,"details":"You are missing the domain parameter"}]}`
	srv := hunterServer(t, http.StatusBadRequest, body, nil)
	defer srv.Close()

	h := &HunterProvider{APIKey: "k", BaseURL: srv.URL, HTTP: srv.Client()}
	_, err := h.FindByDomain(context.Background(), "acme.com")
	if err == nil || !strings.Contains(err.Error(), "missing the domain") {
		t.Errorf("expected Hunter error detail surfaced, got: %v", err)
	}
}

func TestFindByDomain_EmptyDomainRejected(t *testing.T) {
	h := &HunterProvider{APIKey: "k"}
	if _, err := h.FindByDomain(context.Background(), "  "); err == nil {
		t.Error("expected empty-domain rejection")
	}
}

func TestNewHunterFromEnv_RequiresKey(t *testing.T) {
	t.Setenv(EnvAPIKey, "")
	if _, err := NewHunterFromEnv(); err == nil {
		t.Error("expected error when key absent")
	}
	t.Setenv(EnvAPIKey, "secret")
	p, err := NewHunterFromEnv()
	if err != nil {
		t.Fatalf("unexpected error with key set: %v", err)
	}
	if p.APIKey != "secret" {
		t.Errorf("key not loaded from env")
	}
}

func TestMockProvider_RoundTrips(t *testing.T) {
	m := &MockProvider{Result: SampleResult("acme.com")}
	res, err := m.FindByDomain(context.Background(), "acme.com")
	if err != nil {
		t.Fatalf("mock: %v", err)
	}
	if len(res.Contacts) == 0 {
		t.Error("sample result should have contacts")
	}
	// Sample must span multiple departments so the filter has work to do.
	depts := map[types.Department]bool{}
	for _, c := range res.Contacts {
		depts[c.Department] = true
	}
	if len(depts) < 2 {
		t.Errorf("sample should span >= 2 departments, got %d", len(depts))
	}
}

func TestMockProvider_ErrPath(t *testing.T) {
	m := &MockProvider{Err: context.Canceled}
	if _, err := m.FindByDomain(context.Background(), "x"); err == nil {
		t.Error("mock should return configured error")
	}
}

func TestMockProvider_ByDomain(t *testing.T) {
	known := SampleResult("acme.com")
	m := &MockProvider{ByDomain: map[string]Result{"acme.com": known}}

	res, err := m.FindByDomain(context.Background(), "acme.com")
	if err != nil {
		t.Fatalf("known domain: %v", err)
	}
	if len(res.Contacts) != len(known.Contacts) {
		t.Errorf("known domain: got %d contacts, want %d", len(res.Contacts), len(known.Contacts))
	}

	// A domain absent from the map is "looked, found nobody" — an empty,
	// non-error Result — not a fallback to m.Result.
	res, err = m.FindByDomain(context.Background(), "unmapped.com")
	if err != nil {
		t.Fatalf("unmapped domain should not error: %v", err)
	}
	if len(res.Contacts) != 0 {
		t.Errorf("unmapped domain should have no contacts, got %d", len(res.Contacts))
	}
	if res.Organization.Domain != "unmapped.com" {
		t.Errorf("unmapped domain result should echo the domain, got %q", res.Organization.Domain)
	}
}

func TestFindByDomain_UnparseableBodySurfacesStatusAndSnippet(t *testing.T) {
	longGarbage := strings.Repeat("not json ", 100)
	srv := hunterServer(t, http.StatusOK, longGarbage, nil)
	defer srv.Close()

	h := &HunterProvider{APIKey: "k", BaseURL: srv.URL, HTTP: srv.Client()}
	_, err := h.FindByDomain(context.Background(), "acme.com")
	if err == nil {
		t.Fatal("expected error for unparseable body")
	}
	if !strings.Contains(err.Error(), "200") {
		t.Errorf("error should name the status code, got: %v", err)
	}
	// snippet() must bound the body rather than dumping the whole thing.
	if len(err.Error()) > len(longGarbage) {
		t.Errorf("error should be bounded by snippet, got length %d", len(err.Error()))
	}
}

func TestFindByDomain_NonSuccessStatusWithoutErrorsArray(t *testing.T) {
	body := `{"data":{"domain":"acme.com","organization":"","pattern":null,"emails":[]}}`
	srv := hunterServer(t, http.StatusInternalServerError, body, nil)
	defer srv.Close()

	h := &HunterProvider{APIKey: "k", BaseURL: srv.URL, HTTP: srv.Client()}
	_, err := h.FindByDomain(context.Background(), "acme.com")
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("expected status-500 error, got: %v", err)
	}
}
