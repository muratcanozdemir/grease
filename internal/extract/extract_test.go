package extract

import (
	"context"
	"strings"
	"testing"

	"github.com/muratcanozdemir/grease/internal/llm"
	"github.com/muratcanozdemir/grease/internal/types"
)

func TestParse_Valid(t *testing.T) {
	in := `{"company":"Acme Payments","role":"Platform Engineer","tech_stack":["Go","Terraform","AWS"],"seniority":"senior","department":"it"}`
	ext, err := parse(in)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if ext.Company != "Acme Payments" {
		t.Errorf("company = %q", ext.Company)
	}
	if ext.Role != "Platform Engineer" {
		t.Errorf("role = %q", ext.Role)
	}
	if len(ext.TechStack) != 3 || ext.TechStack[0] != "Go" {
		t.Errorf("tech_stack = %v", ext.TechStack)
	}
	if ext.Department != types.DeptIT {
		t.Errorf("department = %q, want it", ext.Department)
	}
}

func TestParse_EmptyCompanyAndStackAllowed(t *testing.T) {
	// Grammar-legal minimal object: shape is fine, role present. Validator must
	// accept it — empty company/stack are not fatal.
	in := `{"company":"","role":"SRE","tech_stack":[],"seniority":"","department":"it"}`
	if _, err := parse(in); err != nil {
		t.Errorf("minimal valid object rejected: %v", err)
	}
}

func TestParse_EmptyRoleRejected(t *testing.T) {
	in := `{"company":"X","role":"","tech_stack":[],"seniority":"","department":"it"}`
	_, err := parse(in)
	if err == nil || !strings.Contains(err.Error(), "role is empty") {
		t.Errorf("expected empty-role rejection, got: %v", err)
	}
}

func TestParse_UnknownFieldRejected(t *testing.T) {
	// DisallowUnknownFields: a stray field means the constraint didn't hold.
	in := `{"company":"X","role":"Dev","tech_stack":[],"seniority":"","department":"it","extra":"nope"}`
	_, err := parse(in)
	if err == nil || !strings.Contains(err.Error(), "not the expected JSON object") {
		t.Errorf("expected unknown-field rejection, got: %v", err)
	}
}

func TestParse_TrailingContentRejected(t *testing.T) {
	in := `{"company":"X","role":"Dev","tech_stack":[],"seniority":"","department":"it"} and then some prose`
	_, err := parse(in)
	if err == nil || !strings.Contains(err.Error(), "trailing content") {
		t.Errorf("expected trailing-content rejection, got: %v", err)
	}
}

func TestParse_MarkdownFenceRejected(t *testing.T) {
	// A model that ignored the grammar and wrapped JSON in a fence must fail,
	// not be salvaged.
	in := "```json\n{\"company\":\"X\",\"role\":\"Dev\",\"tech_stack\":[],\"seniority\":\"\",\"department\":\"it\"}\n```"
	_, err := parse(in)
	if err == nil {
		t.Errorf("expected fenced output to be rejected")
	}
}

func TestParse_InvalidDepartmentRejected(t *testing.T) {
	// Should be impossible under the grammar; the validator is the backstop.
	in := `{"company":"X","role":"Dev","tech_stack":[],"seniority":"","department":"platform"}`
	_, err := parse(in)
	if err == nil || !strings.Contains(err.Error(), "not the expected JSON object") {
		// Note: "platform" is not in the enum, but Department is a string type,
		// so JSON decoding succeeds and the validator catches it. Adjust the
		// expectation: decoding passes, validate fails.
		if err == nil || !strings.Contains(err.Error(), "not a recognized value") {
			t.Errorf("expected invalid-department rejection, got: %v", err)
		}
	}
}

func TestParse_EmptyOutputRejected(t *testing.T) {
	if _, err := parse("   "); err == nil {
		t.Errorf("expected empty output rejection")
	}
}

func TestGrammar_ContainsEveryDepartment(t *testing.T) {
	g := Grammar()
	for _, d := range types.AllDepartments {
		needle := `"` + string(d) + `"`
		if !strings.Contains(g, needle) {
			t.Errorf("grammar missing department literal %s", needle)
		}
	}
}

func TestGrammar_HasAllFields(t *testing.T) {
	g := Grammar()
	for _, field := range []string{"company", "role", "tech_stack", "seniority", "department"} {
		if !strings.Contains(g, `\"`+field+`\"`) {
			t.Errorf("grammar missing field %q", field)
		}
	}
}

// fakeCompleter lets Extract be exercised end-to-end without a model. It records
// whether the constrained path was used and returns a canned payload.
type fakeCompleter struct {
	constrainedCalled bool
	lastGrammar       string
	reply             string
	err               error
}

func (f *fakeCompleter) Complete(ctx context.Context, req llm.Request) (string, error) {
	return "", nil // extraction must never call the unconstrained method
}

func (f *fakeCompleter) CompleteConstrained(ctx context.Context, req llm.Request, grammar string) (string, error) {
	f.constrainedCalled = true
	f.lastGrammar = grammar
	return f.reply, f.err
}

func TestExtract_UsesConstrainedPathAndParses(t *testing.T) {
	fc := &fakeCompleter{
		reply: `{"company":"Acme","role":"SRE","tech_stack":["Go"],"seniority":"senior","department":"it"}`,
	}
	e := New(fc)
	ext, err := e.Extract(context.Background(), "We are hiring an SRE who knows Go.")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !fc.constrainedCalled {
		t.Error("Extract must use the grammar-constrained path")
	}
	if fc.lastGrammar == "" || !strings.Contains(fc.lastGrammar, `"it"`) {
		t.Error("Extract must pass the department-pinned grammar")
	}
	if ext.Role != "SRE" {
		t.Errorf("role = %q", ext.Role)
	}
}

func TestExtract_EmptyJDRejected(t *testing.T) {
	e := New(&fakeCompleter{})
	if _, err := e.Extract(context.Background(), "   "); err == nil {
		t.Error("expected empty-JD rejection")
	}
}

func TestExtract_ModelErrorPropagates(t *testing.T) {
	fc := &fakeCompleter{err: context.DeadlineExceeded}
	e := New(fc)
	_, err := e.Extract(context.Background(), "real JD text")
	if err == nil || !strings.Contains(err.Error(), "model call") {
		t.Errorf("expected model error to propagate, got: %v", err)
	}
}
