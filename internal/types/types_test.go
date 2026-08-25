package types

import "testing"

func TestDepartment_Valid(t *testing.T) {
	for _, d := range AllDepartments {
		if !d.Valid() {
			t.Errorf("%q should be valid: it is in AllDepartments", d)
		}
	}
	if Department("made-up").Valid() {
		t.Error("an unrecognized department should not be valid")
	}
	if Department("").Valid() {
		t.Error("empty string should not be valid")
	}
}

func TestContact_FullName(t *testing.T) {
	cases := []struct {
		name      string
		c         Contact
		wantEmpty bool
		want      string
	}{
		{"both names", Contact{FirstName: "Ada", LastName: "Lovelace"}, false, "Ada Lovelace"},
		{"first only", Contact{FirstName: "Ada"}, false, "Ada"},
		{"last only", Contact{LastName: "Lovelace"}, false, "Lovelace"},
		{"neither", Contact{Email: "info@acme.com"}, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.c.FullName()
			if tc.wantEmpty && got != "" {
				t.Errorf("FullName() = %q, want empty", got)
			}
			if !tc.wantEmpty && got != tc.want {
				t.Errorf("FullName() = %q, want %q", got, tc.want)
			}
		})
	}
}
