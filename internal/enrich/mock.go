package enrich

import (
	"context"
	"fmt"

	"github.com/muratcanozdemir/grease/internal/types"
)

// MockProvider is an EmailProvider that returns fixed data and makes no network
// calls. It exists so the entire grease pipeline — and its test suite — can run
// with no Hunter key and no credit spend.
//
// This is not merely a test fixture: it is the proof that the enrichment
// boundary is real. Because the pipeline depends only on EmailProvider, swapping
// HunterProvider for MockProvider changes nothing upstream or downstream. A
// reviewer can clone the repo and watch the whole thing work end-to-end without
// signing up for anything. That is the boundary doing its job.
type MockProvider struct {
	// Result is returned for any domain. If a test needs per-domain behavior,
	// set ByDomain instead.
	Result Result
	// ByDomain, if non-nil, takes precedence: the lookup returns the Result
	// keyed by the requested domain, or an empty (non-error) Result if absent.
	ByDomain map[string]Result
	// Err, if set, is returned instead of a Result — for exercising the
	// pipeline's error handling.
	Err error
}

// FindByDomain implements EmailProvider.
func (m *MockProvider) FindByDomain(ctx context.Context, domain string) (Result, error) {
	if m.Err != nil {
		return Result{}, m.Err
	}
	if m.ByDomain != nil {
		if r, ok := m.ByDomain[domain]; ok {
			return r, nil
		}
		return Result{Organization: types.Organization{Domain: domain}}, nil
	}
	return m.Result, nil
}

// SampleResult returns a realistic fixture spanning several departments and a
// mix of personal and generic-derived contacts, useful as the default mock
// payload and in documentation/demos. It intentionally includes contacts in
// different departments so the downstream department filter has something to
// actually filter.
func SampleResult(domain string) Result {
	return Result{
		Organization: types.Organization{
			Name:    "Acme Payments",
			Domain:  domain,
			Pattern: "{first}.{last}",
		},
		Contacts: []types.Contact{
			{Email: fmt.Sprintf("ada.lovelace@%s", domain), FirstName: "Ada", LastName: "Lovelace", Position: "VP of Engineering", Seniority: "executive", Department: types.DeptIT, Confidence: 94},
			{Email: fmt.Sprintf("grace.hopper@%s", domain), FirstName: "Grace", LastName: "Hopper", Position: "Head of Platform", Seniority: "senior", Department: types.DeptIT, Confidence: 91},
			{Email: fmt.Sprintf("alan.turing@%s", domain), FirstName: "Alan", LastName: "Turing", Position: "Staff SRE", Seniority: "senior", Department: types.DeptIT, Confidence: 88},
			{Email: fmt.Sprintf("katherine.johnson@%s", domain), FirstName: "Katherine", LastName: "Johnson", Position: "CFO", Seniority: "executive", Department: types.DeptFinance, Confidence: 90},
			{Email: fmt.Sprintf("hedy.lamarr@%s", domain), FirstName: "Hedy", LastName: "Lamarr", Position: "Head of Talent", Seniority: "senior", Department: types.DeptHR, Confidence: 86},
			{Email: fmt.Sprintf("info@%s", domain), FirstName: "", LastName: "", Position: "", Seniority: "", Department: types.DeptUnknown, Confidence: 70},
		},
	}
}

// compile-time assertions that both providers satisfy the interface.
var (
	_ EmailProvider = (*HunterProvider)(nil)
	_ EmailProvider = (*MockProvider)(nil)
)
