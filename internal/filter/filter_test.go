package filter

import (
	"testing"

	"github.com/muratcanozdemir/grease/internal/types"
)

func contacts() []types.Contact {
	return []types.Contact{
		{Email: "a@x.com", Department: types.DeptIT},
		{Email: "b@x.com", Department: types.DeptFinance},
		{Email: "c@x.com", Department: types.DeptIT},
		{Email: "d@x.com", Department: types.DeptHR},
		{Email: "e@x.com", Department: types.DeptUnknown},
	}
}

func TestByDepartment_MatchesTarget(t *testing.T) {
	p := ByDepartment(contacts(), types.DeptIT)
	if len(p.Matched) != 2 {
		t.Fatalf("matched = %d, want 2", len(p.Matched))
	}
	if p.Matched[0].Email != "a@x.com" || p.Matched[1].Email != "c@x.com" {
		t.Errorf("matched wrong or out of order: %+v", p.Matched)
	}
	if len(p.Other) != 3 {
		t.Errorf("other = %d, want 3", len(p.Other))
	}
}

func TestByDepartment_NothingDropped(t *testing.T) {
	all := contacts()
	p := ByDepartment(all, types.DeptIT)
	if len(p.Matched)+len(p.Other) != len(all) {
		t.Errorf("contacts lost: %d + %d != %d", len(p.Matched), len(p.Other), len(all))
	}
}

func TestByDepartment_OrderPreserved(t *testing.T) {
	p := ByDepartment(contacts(), types.DeptIT)
	// Other should be b, d, e in original order.
	wantOther := []string{"b@x.com", "d@x.com", "e@x.com"}
	if len(p.Other) != len(wantOther) {
		t.Fatalf("other len = %d", len(p.Other))
	}
	for i, w := range wantOther {
		if p.Other[i].Email != w {
			t.Errorf("other[%d] = %s, want %s", i, p.Other[i].Email, w)
		}
	}
}

func TestByDepartment_UnknownTargetMatchesNothing(t *testing.T) {
	p := ByDepartment(contacts(), types.DeptUnknown)
	if len(p.Matched) != 0 {
		t.Errorf("unknown target should match nothing, matched %d", len(p.Matched))
	}
	if len(p.Other) != 5 {
		t.Errorf("all should be in other, got %d", len(p.Other))
	}
}

func TestByDepartment_InvalidTargetMatchesNothing(t *testing.T) {
	p := ByDepartment(contacts(), types.Department("platform"))
	if len(p.Matched) != 0 {
		t.Errorf("invalid target should match nothing, matched %d", len(p.Matched))
	}
}

func TestByDepartment_Empty(t *testing.T) {
	p := ByDepartment(nil, types.DeptIT)
	if len(p.Matched) != 0 || len(p.Other) != 0 {
		t.Errorf("empty input should yield empty partition")
	}
}
