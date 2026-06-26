# grease

Turn a company domain, a job posting, and your résumé into ready-to-send `.eml`
files — one per contact you choose. grease finds who to email, drafts a
personalized message grounded in your résumé, attaches the résumé, and writes a
standard `.eml` you open in your own mail client to review and send.

grease sends nothing itself. There is no OAuth, no inbox access, no stored
tokens. It produces files; you send them.

```
grease \
  -domain acme.com \
  -jd "https://boards.example.com/acme/senior-platform-engineer" \
  -resume ./jane-cv.txt \
  -name "Jane Doe" \
  -from jane@example.com
```

## Why this exists, and what it's demonstrating

This is a clean-room reconstruction of a "DM the boss" cold-outreach SaaS,
rebuilt as a portfolio piece. The interesting part is not that it calls an LLM —
anything can call an LLM. The interesting part is **where the LLM is allowed to
act, and where it is structurally prevented from acting.**

The whole system is built on one conviction: a language model belongs at the
edges of a system, not in its control flow. grease has exactly two LLM
touchpoints, and everything between and around them is deterministic, typed, and
testable without a model running.

### The two LLM touchpoints, and why only these two

1. **Extracting structure from the job posting.** A posting is unstructured prose
   in whatever format a job board emits. Hand-writing a parser for that is a
   permanent maintenance liability, so this is genuinely a job for a model. But
   its output is **forced into a typed struct by a grammar** — the model emits
   JSON conforming to a GBNF grammar, the result is validated on receipt, and a
   malformed result is a hard failure. Once that struct exists, the model is out
   of the loop. No prose from the posting propagates further.

2. **Drafting the email body.** Writing a tailored opening line is a language
   task; this call is unconstrained because the desired output is prose. It is
   grounded on the extracted struct, your résumé, and the specific recipient, and
   it produces the body only. The envelope around it — recipient, subject,
   signature, MIME structure, the attachment — is assembled mechanically.

Everything else is deterministic: fetching and stripping the posting, looking up
contacts, partitioning them, the selection UI, and building the `.eml`. None of
it asks a model anything.

### What is *not* an LLM touchpoint, on purpose

- **Resolving the company domain.** The domain is a required input, not something
  a model guesses. A wrong domain silently poisons every downstream contact
  lookup and costs real API credits, so there is no "guess the domain" step. One
  line of input removes an entire class of confident-wrong failures.

- **Ranking contacts.** grease does not score or rank people. The extraction node
  emits a department guess (mapping e.g. "Platform Engineer" → `it`), and a
  deterministic filter does a plain equality partition against that guess.
  Matches surface first, everyone else below a divider, nobody is dropped. There
  is no weighting and no rule engine to maintain — the interpretive judgment
  lives in the model where it's cheap, and the filter is a one-line comparison.

- **Retrying a failed call.** There are no retry loops anywhere. A failed model
  call, a malformed extraction, a rate-limited API — each fails loudly and stops.
  A retry that feeds a model its own bad output back is the agent-in-control-flow
  pattern this design exists to avoid, and an un-throttled API retry is the exact
  behavior that gets keys suspended.

## The enrichment boundary

Contact discovery is the part of any tool like this that is legally loaded
(where does contact data come from?), costs money (per-lookup API credits), and
talks to a third party you don't control. grease puts that entire concern behind
one narrow interface with more than one implementation:

```go
type EmailProvider interface {
    FindByDomain(ctx context.Context, domain string) (Result, error)
}
```

- `HunterProvider` makes real, credited calls to Hunter's Domain Search API,
  bring-your-own-key (read directly from `HUNTER_API_KEY` in your shell — never
  stored, never written to a dotfile).
- `MockProvider` returns fixed data and touches no network.

The rest of grease depends only on the interface and never knows which is wired.
That is what lets you **clone this repo and run the entire thing end-to-end with
`-mock`, no Hunter account, no credentials, no spend.** It's also what would let
a different provider — or a self-hosted contact source — drop in without the
pipeline noticing. The part that carries the legal and financial weight is
contained at one swappable seam, not woven through the system.

grease does **not** scrape LinkedIn or anything else. Discovery is a single
Domain Search call against a paid, consented data source. It is a single-shot
tool, not a bulk processor: one lookup per run.

## The model backend

The default backend is a local [llama.cpp](https://github.com/ggml-org/llama.cpp)
server, targeting a CPU-only workstation running a non-thinking instruct model
(e.g. Qwen2.5-7B-Instruct, Llama 3.1-8B). Thinking models are a poor fit for an
interactive tool — hidden reasoning tokens inflate time-to-first-token
unpredictably.

The model is reached through a two-method interface:

```go
type Completer interface {
    Complete(ctx, req) (string, error)              // unconstrained — drafting
    CompleteConstrained(ctx, req, grammar) (string, error) // GBNF — extraction
}
```

The constrained method uses llama.cpp's native `/completion` endpoint with a raw
GBNF grammar, so the grammar is hand-authored and sent verbatim — no
JSON-schema-to-grammar conversion in the path. The extraction grammar is
**generated from the department enum in Go**, so the grammar and the type system
cannot drift: add a department to the Go type and the grammar's allowed values
change with it. This is what makes "the model cannot emit an unknown department"
a structural guarantee rather than a hope.

Provider-agnosticism (cloud models, multi-provider routing) is intentionally out
of scope: it's a router's job, and the interface already allows it by supplying a
different `Completer`. The default ships pointed at localhost.

The shipped prompt framing is ChatML (Qwen2.5-compatible), behind a `Template`
interface. A model with a different template means swapping that one
implementation; a mismatch degrades draft phrasing but cannot corrupt the
extraction struct's shape, because the grammar enforces shape independently of
the template.

## Flags

| Flag | Meaning |
|------|---------|
| `-domain` | Company domain to find contacts at, e.g. `acme.com` (required) |
| `-jd` | Job description: raw text, or an `http(s)` URL to a posting (required) |
| `-resume` | Path to your résumé, attached to each email (required) |
| `-name` | Your name, used to sign the emails (required) |
| `-from` | Your email address, used in the `From` header (required) |
| `-out` | Directory to write `.eml` files into (default `./grease-out`) |
| `-llm` | Base URL of the llama.cpp server (default `http://localhost:8080`) |
| `-mock` | Use the mock contact provider — no Hunter key, no network, no credits |

`HUNTER_API_KEY` must be set in your environment unless you pass `-mock`.

### A note on résumé grounding

The résumé is always attached as-is. For the *drafting* step, a plain-text
(`.txt`/`.md`) résumé grounds the model directly with your real experience. A PDF
or `.docx` still attaches correctly, but its bytes aren't useful text for
grounding — supply a `.txt` version via `-resume` if you want the draft to draw
on specific résumé details. (Extracting text from PDF/docx is deliberately out of
scope; it's a separable concern and not what this project is demonstrating.)

## Architecture

```
internal/
  types/    Domain vocabulary. Department enum pinned to Hunter's set.
            The single source of truth the extraction grammar is generated from.
  llm/      Completer interface + llama.cpp /completion adapter (native, raw GBNF).
  prompt/   Template interface + ChatML default.
  jd/       Deterministic JD ingestion: raw text, or URL fetch + markup strip.
  extract/  LLM node 1. Grammar (generated from the enum), prompt, strict parse,
            hard-fail validation. No retry loop.
  enrich/   EmailProvider interface + Hunter adapter (BYOK) + mock adapter.
  filter/   Deterministic department partition. No scoring, no rules.
  draft/    LLM node 2. Unconstrained body generation, grounded; deterministic subject.
  emit/     Deterministic RFC 5322 / MIME .eml assembly with base64 résumé attachment.
cmd/grease/ CLI: flags, provider switch, the interactive selection, wiring.
```

Data flow:

```
JD (text or URL) ──ingest──▶ text
text             ──LLM:extract (grammar)──▶ typed struct ──┐
domain           ──enrich (Hunter | mock)──▶ contacts ─────┤
                                                           ├──filter──▶ matched / other
                                                  you ──select──▶ chosen
chosen + struct  ──LLM:draft (unconstrained)──▶ body
body + résumé    ──emit (deterministic MIME)──▶ .eml on disk
```

## Building and testing

```
go build ./...
go test ./...           # entire suite runs against mocks — no model, no network, no key
go build -o grease ./cmd/grease
```

The test suite is fully self-contained. The LLM adapter is tested against an
`httptest` server mimicking llama.cpp's wire format; the Hunter adapter against
canned responses covering the auth, rate-limit, and error-array paths; the
emitter by parsing its own output back as MIME and asserting the résumé bytes
round-trip. None of it requires a running model, a network, or a Hunter key.

To smoke-test against a real model, point `-llm` at a running `llama-server` and
run with `-mock` (no Hunter key needed). If the extraction grammar were ever
malformed, llama.cpp returns a "Failed to parse grammar" error, which the adapter
surfaces verbatim — failures are loud and immediately diagnosable.

## Scope boundaries (what this deliberately is not)

- Not a bulk outreach tool. One domain, one run, one lookup.
- Not a scraper. Consented data source only.
- Not an email sender. It writes `.eml` files; you send them.
- Not a PDF parser. Résumés attach as-is; text grounding wants plain text.
- Not provider-agnostic out of the box. The seams allow it; the defaults are local.
```
