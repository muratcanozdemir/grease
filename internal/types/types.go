// Package types holds the domain values that flow through the grease pipeline.
//
// These types are the contract between stages. The LLM nodes (extract, draft)
// emit and consume these as typed values; everything between them is
// deterministic and never sees free-form model prose. Keeping the vocabulary in
// one dependency-free package is what lets the rest of the system stay honest
// about where the model's influence starts and stops.
package types

// Department is the closed set of organizational functions grease recognizes.
//
// This is deliberately Hunter's department vocabulary, not ours. The extraction
// node's GBNF grammar pins the model's department output to exactly these
// values, and the same values are what Hunter's Domain Search returns on each
// contact. Because both sides speak the same enum, the downstream filter is a
// plain equality check with nothing to interpret and nothing to maintain.
//
// Source: Hunter Domain Search API `department` field.
type Department string

const (
	DeptExecutive     Department = "executive"
	DeptIT            Department = "it"
	DeptFinance       Department = "finance"
	DeptManagement    Department = "management"
	DeptSales         Department = "sales"
	DeptLegal         Department = "legal"
	DeptSupport       Department = "support"
	DeptHR            Department = "hr"
	DeptMarketing     Department = "marketing"
	DeptCommunication Department = "communication"
	DeptEducation     Department = "education"
	DeptDesign        Department = "design"
	DeptHealth        Department = "health"
	DeptOperations    Department = "operations"
	// DeptUnknown is the grammar's escape hatch: when the JD gives no usable
	// signal about which function the role sits in, the model emits this rather
	// than guessing. The filter treats it as "matches nothing", so contacts are
	// never surfaced on a fabricated department.
	DeptUnknown Department = "unknown"
)

// AllDepartments is the authoritative list, used to build the extraction
// grammar and to validate model output. Order is irrelevant; presence is not.
var AllDepartments = []Department{
	DeptExecutive, DeptIT, DeptFinance, DeptManagement, DeptSales,
	DeptLegal, DeptSupport, DeptHR, DeptMarketing, DeptCommunication,
	DeptEducation, DeptDesign, DeptHealth, DeptOperations, DeptUnknown,
}

// Valid reports whether d is a recognized department. Output from the model is
// checked against this on receipt; an unrecognized value is a hard failure, not
// something to coerce.
func (d Department) Valid() bool {
	for _, known := range AllDepartments {
		if d == known {
			return true
		}
	}
	return false
}

// Extraction is the typed result of LLM node 1 (the JD parser).
//
// This is the only structured thing the model produces from a job description.
// Once validated, the pipeline consumes this struct and the model is out of the
// loop until drafting. Free text from the JD does not propagate past this point.
type Extraction struct {
	// Company is the organization name as the JD presents it. Informational;
	// the domain (which Hunter actually needs) is user-supplied, never inferred
	// here, because a wrong domain silently poisons every enrichment call.
	Company string `json:"company"`

	// Role is the job title as written in the posting.
	Role string `json:"role"`

	// TechStack is the normalized set of technologies named or implied by the
	// JD. Used to ground the drafting node so the email speaks to the actual
	// posting rather than generic filler.
	TechStack []string `json:"tech_stack"`

	// Seniority is a coarse signal extracted from the posting (e.g. "senior",
	// "lead", "staff"). Free-form on purpose; nothing downstream branches on it,
	// it only colors the draft.
	Seniority string `json:"seniority"`

	// Department is the model's mapping of this role onto Hunter's department
	// vocabulary — e.g. "Platform Engineer" -> it. This is where the
	// interpretation lives. The grammar constrains it to the enum so the model
	// cannot invent a value the filter wouldn't understand.
	Department Department `json:"department"`
}

// Contact is one person grease can reach out to, as returned by the enrichment
// provider. Field names mirror Hunter's Domain Search response so the adapter
// is a thin mapping rather than a translation layer.
type Contact struct {
	Email      string     `json:"email"`
	FirstName  string     `json:"first_name"`
	LastName   string     `json:"last_name"`
	Position   string     `json:"position"`
	Seniority  string     `json:"seniority"`
	Department Department `json:"department"`
	// Confidence is Hunter's 0–100 estimate that the address is correct. Carried
	// through for display so the user can weigh who to contact; grease does not
	// threshold or rank on it.
	Confidence int `json:"confidence"`
}

// FullName returns the contact's name for salutation purposes, tolerating
// missing parts. Hunter frequently has one half of a name and not the other.
func (c Contact) FullName() string {
	switch {
	case c.FirstName != "" && c.LastName != "":
		return c.FirstName + " " + c.LastName
	case c.FirstName != "":
		return c.FirstName
	case c.LastName != "":
		return c.LastName
	default:
		return ""
	}
}

// Organization is the company-level data Hunter returns alongside contacts on a
// Domain Search. Used to give the draft a little more grounding (and to show the
// user what domain actually resolved to).
type Organization struct {
	Name    string `json:"name"`
	Domain  string `json:"domain"`
	Pattern string `json:"pattern"` // e.g. "{first}.{last}", informational only
}
