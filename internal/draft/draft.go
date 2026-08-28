// Package draft is LLM node 2: it writes a cold outreach email body for one
// contact.
//
// This is the second and last place the model touches the pipeline, and unlike
// extraction it is unconstrained — the desired output is free prose, an email
// body, so it calls Complete rather than CompleteConstrained. The model is
// grounded on three things: the structured facts extracted from the JD, the
// applicant's resume, and the specific person being written to. It is asked for
// the body only; the envelope (recipient, subject scaffolding, signature, resume
// attachment) is assembled deterministically downstream in the emit package.
//
// Keeping the model to just the body is the boundary discipline applied once
// more: the part that benefits from natural language (a tailored opening, a
// relevant phrasing) is the model's; everything mechanical around it is not.
package draft

import (
	"context"
	"fmt"
	"strings"

	"github.com/muratcanozdemir/grease/internal/llm"
	"github.com/muratcanozdemir/grease/internal/prompt"
	"github.com/muratcanozdemir/grease/internal/types"
)

// Input is everything the drafting node needs for one email.
type Input struct {
	Extraction types.Extraction
	Org        types.Organization
	Contact    types.Contact
	// ResumeText is the applicant's resume as plain text. The model uses it to
	// ground claims in real experience rather than inventing them.
	ResumeText string
	// SenderName is how the applicant signs off. Used in the prompt so the model
	// doesn't hallucinate a name.
	SenderName string
}

// Drafter runs the drafting node against a Completer.
type Drafter struct {
	LLM         llm.Completer
	Template    prompt.Template
	Temperature float64
	MaxTokens   int
}

// New builds a Drafter with defaults. Temperature is modest — some warmth in
// phrasing is wanted, but not so much that the email drifts off the facts.
func New(model llm.Completer) *Drafter {
	return &Drafter{
		LLM:         model,
		Template:    prompt.ChatML{},
		Temperature: 0.6,
		MaxTokens:   600,
	}
}

const draftSystem = `You write concise, specific cold outreach emails from a job applicant to a person at a company they want to work for.

Rules:
- Write ONLY the email body. No subject line, no "Subject:", no greeting line beyond the salutation, no sign-off signature block beyond a simple closing and the sender's name.
- Keep it short: three short paragraphs at most. Hiring managers skim.
- Open by addressing the recipient by name if one is given.
- Reference the specific role and one or two concrete, relevant points from the applicant's background that match what the role needs. Use the resume — do not invent experience the resume does not support.
- Be direct about the ask: a brief conversation about the role.
- Plain, professional tone. No flattery, no buzzwords, no "I am thrilled". Do not exaggerate.
- Do not mention that this email was generated, and do not include placeholders like [Company] — use the actual details provided.`

// Draft produces the email body for one contact.
func (d *Drafter) Draft(ctx context.Context, in Input) (string, error) {
	if strings.TrimSpace(in.ResumeText) == "" {
		return "", fmt.Errorf("draft: empty resume text")
	}
	if strings.TrimSpace(in.Extraction.Role) == "" {
		return "", fmt.Errorf("draft: extraction has no role")
	}

	tmpl := d.Template
	if tmpl == nil {
		tmpl = prompt.ChatML{}
	}

	user := buildUserPrompt(in)
	req := llm.Request{
		Prompt:      tmpl.Render(draftSystem, user),
		Temperature: d.Temperature,
		MaxTokens:   d.MaxTokens,
		Stop:        tmpl.Stop(),
	}

	body, err := d.LLM.Complete(ctx, req)
	if err != nil {
		return "", fmt.Errorf("draft: model call: %w", err)
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return "", fmt.Errorf("draft: model returned empty body")
	}
	return body, nil
}

// buildUserPrompt assembles the grounding context the model sees. Structured
// and explicit so the model has the facts without having to infer them.
func buildUserPrompt(in Input) string {
	var b strings.Builder

	company := in.Extraction.Company
	if company == "" {
		company = in.Org.Name
	}

	b.WriteString("Write the email body.\n\n")
	b.WriteString("ROLE: ")
	b.WriteString(in.Extraction.Role)
	b.WriteString("\nCOMPANY: ")
	b.WriteString(company)
	if len(in.Extraction.TechStack) > 0 {
		b.WriteString("\nROLE FOCUSES ON: ")
		b.WriteString(strings.Join(in.Extraction.TechStack, ", "))
	}
	if in.Extraction.Seniority != "" {
		b.WriteString("\nSENIORITY: ")
		b.WriteString(in.Extraction.Seniority)
	}

	b.WriteString("\n\nRECIPIENT:\n")
	if name := in.Contact.FullName(); name != "" {
		b.WriteString("- Name: ")
		b.WriteString(name)
		b.WriteString("\n")
	} else {
		b.WriteString("- Name: (unknown — use a neutral greeting like \"Hello\")\n")
	}
	if in.Contact.Position != "" {
		b.WriteString("- Position: ")
		b.WriteString(in.Contact.Position)
		b.WriteString("\n")
	}

	b.WriteString("\nSENDER NAME (sign off as this): ")
	b.WriteString(in.SenderName)

	b.WriteString("\n\nAPPLICANT RESUME (ground the email in this; do not invent beyond it):\n")
	b.WriteString(in.ResumeText)

	return b.String()
}

// SuggestSubject builds a deterministic, non-LLM subject line. The subject is
// formulaic enough that a model adds nothing but variance; keeping it
// deterministic means it is predictable and never hallucinated. The user can
// edit it before sending regardless.
func SuggestSubject(in Input) string {
	role := in.Extraction.Role
	if role == "" {
		role = "your open role"
	}
	if in.SenderName != "" {
		return fmt.Sprintf("Re: %s — %s", role, in.SenderName)
	}
	return fmt.Sprintf("Re: %s", role)
}
