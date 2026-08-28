package draft

import (
	"context"
	"strings"
	"testing"

	"github.com/muratcanozdemir/grease/internal/llm"
	"github.com/muratcanozdemir/grease/internal/types"
)

type fakeCompleter struct {
	completeCalled    bool
	constrainedCalled bool
	lastPrompt        string
	reply             string
	err               error
}

func (f *fakeCompleter) Complete(ctx context.Context, req llm.Request) (string, error) {
	f.completeCalled = true
	f.lastPrompt = req.Prompt
	return f.reply, f.err
}

func (f *fakeCompleter) CompleteConstrained(ctx context.Context, req llm.Request, grammar string) (string, error) {
	f.constrainedCalled = true
	return "", nil
}

func sampleInput() Input {
	return Input{
		Extraction: types.Extraction{
			Company:   "Acme Payments",
			Role:      "Senior Platform Engineer",
			TechStack: []string{"Go", "Terraform", "AWS"},
			Seniority: "senior",
		},
		Org:        types.Organization{Name: "Acme Payments", Domain: "acme.com"},
		Contact:    types.Contact{FirstName: "Ada", LastName: "Lovelace", Position: "VP Engineering", Department: types.DeptIT},
		ResumeText: "10 years running production Kubernetes. Go controllers and CRDs at scale.",
		SenderName: "Jane Doe",
	}
}

func TestDraft_UsesUnconstrainedPath(t *testing.T) {
	fc := &fakeCompleter{reply: "Hi Ada,\n\nI'd love to talk about the Platform Engineer role.\n\nBest,\nJane"}
	d := New(fc)
	body, err := d.Draft(context.Background(), sampleInput())
	if err != nil {
		t.Fatalf("Draft: %v", err)
	}
	if !fc.completeCalled {
		t.Error("Draft must use the unconstrained Complete path")
	}
	if fc.constrainedCalled {
		t.Error("Draft must NOT use the constrained path")
	}
	if !strings.Contains(body, "Ada") {
		t.Errorf("body = %q", body)
	}
}

func TestDraft_GroundsPromptInFacts(t *testing.T) {
	fc := &fakeCompleter{reply: "ok"}
	d := New(fc)
	if _, err := d.Draft(context.Background(), sampleInput()); err != nil {
		t.Fatal(err)
	}
	p := fc.lastPrompt
	// The prompt must carry the concrete grounding the model needs.
	for _, want := range []string{"Senior Platform Engineer", "Acme Payments", "Go", "Ada Lovelace", "Jane Doe", "production Kubernetes"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing grounding %q", want)
		}
	}
}

func TestDraft_RejectsEmptyResume(t *testing.T) {
	in := sampleInput()
	in.ResumeText = "  "
	d := New(&fakeCompleter{reply: "x"})
	if _, err := d.Draft(context.Background(), in); err == nil {
		t.Error("expected empty-resume rejection")
	}
}

func TestDraft_RejectsEmptyModelOutput(t *testing.T) {
	d := New(&fakeCompleter{reply: "   "})
	if _, err := d.Draft(context.Background(), sampleInput()); err == nil {
		t.Error("expected empty-output rejection")
	}
}

func TestDraft_PropagatesModelError(t *testing.T) {
	d := New(&fakeCompleter{err: context.DeadlineExceeded})
	_, err := d.Draft(context.Background(), sampleInput())
	if err == nil || !strings.Contains(err.Error(), "model call") {
		t.Errorf("expected model error, got: %v", err)
	}
}

func TestSuggestSubject(t *testing.T) {
	s := SuggestSubject(sampleInput())
	if !strings.Contains(s, "Senior Platform Engineer") || !strings.Contains(s, "Jane Doe") {
		t.Errorf("subject = %q", s)
	}
}
