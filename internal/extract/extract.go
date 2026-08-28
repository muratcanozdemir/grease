// Package extract is LLM node 1: it turns a job description into a typed
// Extraction struct.
//
// This is the only place a job description's free text influences the pipeline.
// The model reads the JD and emits a structured value; once that value is
// validated, the model is out of the loop until drafting, and nothing
// downstream ever sees JD prose again.
//
// Two mechanisms keep the node honest:
//
//   - A hand-written GBNF grammar forces the output to be a JSON object with
//     exactly the expected fields, and pins the department field to grease's
//     known department vocabulary. The model's sampler physically cannot emit a
//     department value outside that set. The grammar is generated from
//     types.AllDepartments, so the grammar and the Go enum cannot drift apart.
//
//   - On receipt, the decoded struct is validated and a bad one is a hard
//     failure. The grammar guarantees shape, not sense: it permits an empty
//     company string or an empty tech-stack array. Validation, not blind trust
//     in the constraint, is what guards value quality. There is deliberately no
//     retry loop — a retry that feeds the model its own bad output back is the
//     agent-in-control-flow pattern this architecture rejects. One call,
//     validate, fail loud.
package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/muratcanozdemir/grease/internal/llm"
	"github.com/muratcanozdemir/grease/internal/prompt"
	"github.com/muratcanozdemir/grease/internal/types"
)

// systemInstruction frames the task. Because the native endpoint's grammar is
// not shown to the model (llama.cpp constrains sampling, it does not inject the
// schema into the prompt), the expected structure is described here in plain
// language. The grammar enforces; this text informs. The two must agree — if a
// field is added to the grammar, it is described here too.
const systemInstruction = `You extract structured hiring information from job descriptions.
You will be given the text of a job posting. Respond with a single JSON object and nothing else — no explanation, no markdown.

The JSON object must have exactly these fields:
- "company": the hiring organization's name as written in the posting (string; empty string if not stated).
- "role": the job title as written (string).
- "tech_stack": the technologies, languages, frameworks, and tools named or clearly implied by the posting (array of short strings; empty array if none are identifiable).
- "seniority": a short phrase for the seniority level if indicated, e.g. "junior", "senior", "staff", "lead" (string; empty string if not indicated).
- "department": which organizational function this role belongs to, chosen from this fixed list ONLY:
  executive, it, finance, management, sales, legal, support, hr, marketing, communication, education, design, health, operations, unknown.
  Map the role to the closest function. For most engineering, software, platform, devops, SRE, or infrastructure roles, choose "it". Use "unknown" only when the posting gives no usable signal.`

// Extractor runs the extraction node against a Completer.
type Extractor struct {
	LLM      llm.Completer
	Template prompt.Template
	// Temperature for extraction. Low by default at the call site; extraction is
	// a parsing task, not a creative one, so determinism is preferred.
	Temperature float64
	// MaxTokens caps the JSON output. A JD extraction is small; this guards
	// against a runaway generation while leaving ample room for a long
	// tech_stack.
	MaxTokens int
}

// New builds an Extractor with sensible defaults: ChatML template, temperature
// 0 (greedy — the most reproducible setting for a parsing task), and a token
// cap generous enough for the struct.
func New(model llm.Completer) *Extractor {
	return &Extractor{
		LLM:         model,
		Template:    prompt.ChatML{},
		Temperature: 0,
		MaxTokens:   1024,
	}
}

// Extract runs the node: build prompt, call under grammar, parse, validate.
func (e *Extractor) Extract(ctx context.Context, jdText string) (types.Extraction, error) {
	if strings.TrimSpace(jdText) == "" {
		return types.Extraction{}, fmt.Errorf("extract: empty job description")
	}

	tmpl := e.Template
	if tmpl == nil {
		tmpl = prompt.ChatML{}
	}

	full := tmpl.Render(systemInstruction, jdText)
	grammar := Grammar()

	req := llm.Request{
		Prompt:      full,
		Temperature: e.Temperature,
		MaxTokens:   e.MaxTokens,
		Stop:        tmpl.Stop(),
	}

	out, err := e.LLM.CompleteConstrained(ctx, req, grammar)
	if err != nil {
		return types.Extraction{}, fmt.Errorf("extract: model call: %w", err)
	}

	ext, err := parse(out)
	if err != nil {
		return types.Extraction{}, err
	}
	return ext, nil
}

// parse decodes and validates the model's constrained output. Kept separate so
// it is unit-testable without a model: the grammar's correctness is one
// concern, the parser's strictness is another.
func parse(raw string) (types.Extraction, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return types.Extraction{}, fmt.Errorf("extract: model returned empty output")
	}

	// The grammar should guarantee clean JSON, but the parser does not assume
	// the grammar was applied correctly — a misconfigured server, or a future
	// template change, could let stray text through. Decode strictly and reject
	// anything that does not fit, rather than fishing a JSON object out of prose
	// (which would be exactly the kind of lenient salvage that masks real
	// failures).
	dec := json.NewDecoder(strings.NewReader(s))
	dec.DisallowUnknownFields()

	var ext types.Extraction
	if err := dec.Decode(&ext); err != nil {
		return types.Extraction{}, fmt.Errorf("extract: output is not the expected JSON object: %w (got: %s)", err, truncate(s, 200))
	}
	// Reject trailing content after the JSON object. A well-formed response is
	// exactly one object; anything after it means the constraint did not hold.
	if dec.More() {
		return types.Extraction{}, fmt.Errorf("extract: output contained trailing content after JSON object")
	}

	if err := validate(ext); err != nil {
		return types.Extraction{}, err
	}
	return ext, nil
}

// validate enforces the value-quality rules the grammar cannot express.
func validate(ext types.Extraction) error {
	if strings.TrimSpace(ext.Role) == "" {
		return fmt.Errorf("extract: role is empty (a posting without a role is unusable)")
	}
	if !ext.Department.Valid() {
		// Should be impossible under the grammar, but the enum is the
		// authority; an invalid value here means the grammar and types drifted
		// or the constraint was bypassed. Fail rather than pass garbage to the
		// downstream filter.
		return fmt.Errorf("extract: department %q is not a recognized value", ext.Department)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
