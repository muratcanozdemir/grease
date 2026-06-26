// Package llm defines the seam between grease and whatever language model backs
// its two LLM nodes (JD extraction and email drafting).
//
// The entire model surface is this interface. Both nodes depend on it, not on
// any concrete provider. The shipped implementation talks to a local llama.cpp
// server; pointing grease at a cloud model, or at a multi-provider router, is a
// matter of supplying a different Completer, with no change to the pipeline.
// That swappability is deliberate: the model is a replaceable edge component,
// not load-bearing structure.
package llm

import "context"

// Completer is the one thing the rest of grease knows about language models.
//
// The two methods correspond to the two genuinely different things grease asks
// of a model, and the split is explicit rather than hidden behind a nullable
// option:
//
//   - Complete runs an unconstrained generation. The drafting node uses this to
//     write an email body — free prose is the desired output.
//
//   - CompleteConstrained runs a generation whose output is forced to conform
//     to a supplied GBNF grammar. The extraction node uses this to turn a job
//     description into a typed struct, with the department field pinned to a
//     known vocabulary. The grammar constrains token sampling directly: the
//     sampler physically cannot emit a token that violates it.
//
// Conformance is still the caller's to verify on receipt. A grammar guarantees
// the output matches the grammar; it does not guarantee the result is a
// complete, sensible value (a grammar permitting any JSON object does not stop
// an empty one). So the extraction node validates the decoded struct and
// hard-fails on a bad one rather than trusting the constraint blindly.
//
// Implementations return an error on transport failure, non-success status, or
// a backend that cannot honor the requested grammar. An implementation that
// cannot constrain output must make CompleteConstrained fail loudly rather than
// fall back to unconstrained generation — silently dropping the grammar is the
// failure mode this whole design exists to prevent.
type Completer interface {
	Complete(ctx context.Context, req Request) (string, error)
	CompleteConstrained(ctx context.Context, req Request, grammar string) (string, error)
}

// Request is a single generation call.
//
// Prompt is the fully-assembled instruction string. grease builds the prompt
// itself (including any model-specific chat framing) rather than passing a
// structured message list, because the shipped backend's constrained endpoint
// is the raw-completion one, which takes a single string. Keeping the prompt as
// a plain string keeps the interface honest about that.
type Request struct {
	Prompt      string
	Temperature float64
	MaxTokens   int
	// Stop is an optional set of stop strings. The backend halts generation at
	// the first match and excludes it from the returned text.
	Stop []string
}
