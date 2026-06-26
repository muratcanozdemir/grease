// Package filter partitions enrichment contacts by department.
//
// This is a deterministic node, intentionally dumb. The extraction node already
// did the interpretive work — mapping the role onto a department from Hunter's
// vocabulary. All this does is a plain equality partition: contacts whose
// department equals the extracted guess are "matched", the rest are "other".
// Nothing is scored, weighted, or ranked. There is no rule engine to maintain
// because there are no rules beyond equality, and the vocabulary on both sides
// is the same pinned enum, so the comparison is exact.
//
// The interpretive flexibility lives in the model (Platform Engineer -> it); the
// determinism lives here (it == it). That split is the whole point: the part
// that needs judgment is where judgment is cheap, and the part that needs to be
// predictable is a one-line comparison.
package filter

import "github.com/user/grease/internal/types"

// Partition is the result of splitting contacts against a target department.
type Partition struct {
	// Matched contains contacts whose department equals the target. Order is
	// preserved from the input.
	Matched []types.Contact
	// Other contains every other contact, order preserved. Nothing is dropped —
	// the user still sees everyone, just grouped.
	Other []types.Contact
}

// ByDepartment partitions contacts into those matching target and the rest.
//
// An "unknown" or unrecognized target matches nothing (everyone lands in
// Other), because surfacing contacts on a non-signal would be misleading — when
// extraction couldn't determine the function, grease doesn't pretend it could.
func ByDepartment(contacts []types.Contact, target types.Department) Partition {
	var p Partition
	matchable := target.Valid() && target != types.DeptUnknown
	for _, c := range contacts {
		if matchable && c.Department == target {
			p.Matched = append(p.Matched, c)
		} else {
			p.Other = append(p.Other, c)
		}
	}
	return p
}
