package extract

import (
	"fmt"
	"strings"

	"github.com/user/grease/internal/types"
)

// Grammar returns the GBNF that constrains extraction output.
//
// It is generated rather than hardcoded so the department alternation is built
// directly from types.AllDepartments. If a department is added to or removed
// from the enum, the grammar changes with it automatically — the single source
// of truth for the department vocabulary is the Go type, and the grammar is a
// projection of it. This is the mechanism that makes "the model cannot emit an
// unknown department" a structural guarantee rather than a hope.
//
// The grammar forces a JSON object with exactly the five fields, in a fixed
// order, with correct types. Strings use a standard JSON-string rule; the
// department field is restricted to the quoted enum members. Whitespace is
// permitted between tokens so the model can format naturally.
//
// What the grammar does NOT enforce is value sense — a permitted output is
// {"company":"","role":"","tech_stack":[],"seniority":"","department":"unknown"}.
// That is intentional: shape is the grammar's job, value quality is the
// validator's. Keeping the grammar permissive on values avoids over-constraining
// the model into contortions and keeps the two responsibilities cleanly split.
func Grammar() string {
	var depts []string
	for _, d := range types.AllDepartments {
		// Each department becomes a quoted literal alternative. The values are
		// known-safe (lowercase ascii, no quotes or backslashes), so no escaping
		// is needed, but quote them explicitly for the GBNF string literal.
		depts = append(depts, fmt.Sprintf("%q", string(d)))
	}
	departmentRule := strings.Join(depts, " | ")

	// GBNF. Notes:
	//   - `root` fixes field order and the object structure.
	//   - `string` is the standard JSON string production.
	//   - `strarray` is a possibly-empty array of strings.
	//   - `ws` allows insignificant whitespace.
	//   - `department` is the enum alternation, the load-bearing constraint.
	return `root   ::= "{" ws
  "\"company\":" ws string ws "," ws
  "\"role\":" ws string ws "," ws
  "\"tech_stack\":" ws strarray ws "," ws
  "\"seniority\":" ws string ws "," ws
  "\"department\":" ws department ws
  "}" ws

department ::= ` + departmentRule + `

strarray ::= "[" ws "]" | "[" ws string (ws "," ws string)* ws "]"

string ::= "\"" (
    [^"\\\x7F\x00-\x1F] |
    "\\" (["\\bfnrt] | "u" [0-9a-fA-F]{4})
  )* "\"" ws

ws ::= [ \t\n\r]*
`
}
