// Package enrich is the contact-discovery layer: given a company domain, return
// the people grease could email.
//
// This is the most consequential boundary in the system. Contact enrichment is
// the part that costs money (per-lookup API credits), carries legal weight (data
// provenance, the line between public business contacts and scraping), and is
// the only piece that talks to a third party grease does not control. So it
// lives behind a single narrow interface with more than one implementation:
//
//   - HunterProvider makes real, credited calls to Hunter's Domain Search API.
//   - MockProvider returns fixed data and touches no network.
//
// The rest of grease depends only on the EmailProvider interface. It never
// knows or cares which is wired. That is what lets the whole pipeline run, and
// be tested, with zero credentials and zero spend — and what would let a
// different provider (or a self-hosted source) be dropped in without the
// pipeline noticing. The radioactive layer is contained, not load-bearing.
package enrich

import (
	"context"

	"github.com/muratcanozdemir/grease/internal/types"
)

// Result is what a lookup returns: the contacts found for a domain plus the
// organization-level data the provider reported alongside them.
type Result struct {
	Organization types.Organization
	Contacts     []types.Contact
}

// EmailProvider resolves a company domain to a set of contactable people.
//
// The contract is deliberately minimal — one method, one input, one output —
// because a narrow interface is what keeps providers interchangeable. Anything
// provider-specific (credit accounting, pagination, rate-limit handling, the
// distinction between personal and generic addresses) is the implementation's
// concern and does not leak into this signature.
//
// Implementations return an error on transport failure, authentication failure,
// or an unparseable response. A domain with simply no known contacts is not an
// error: it returns a Result with an empty Contacts slice, so the caller can
// distinguish "looked, found nobody" from "the lookup failed".
type EmailProvider interface {
	FindByDomain(ctx context.Context, domain string) (Result, error)
}
