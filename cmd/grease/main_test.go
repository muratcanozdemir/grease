package main

import (
	"errors"
	"flag"
	"strings"
	"testing"

	"github.com/muratcanozdemir/grease/internal/types"
)

func validArgs() []string {
	return []string{
		"-domain", "acme.com",
		"-jd", "we need a platform engineer",
		"-resume", "resume.txt",
		"-name", "Jane Doe",
		"-from", "jane@example.com",
	}
}

func TestParseFlags_AllRequiredPresent(t *testing.T) {
	cfg, err := parseFlags(validArgs())
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if cfg.domain != "acme.com" || cfg.senderAddr != "jane@example.com" {
		t.Errorf("cfg not populated from flags: %+v", cfg)
	}
	// Defaults.
	if cfg.outDir != "./grease-out" {
		t.Errorf("outDir default = %q", cfg.outDir)
	}
	if cfg.llmURL != "http://localhost:8080" {
		t.Errorf("llmURL default = %q", cfg.llmURL)
	}
	if cfg.useMock {
		t.Error("useMock should default to false")
	}
}

func TestParseFlags_MissingRequiredListsAll(t *testing.T) {
	_, err := parseFlags(nil)
	if err == nil {
		t.Fatal("expected error for missing required flags")
	}
	for _, want := range []string{"-domain", "-jd", "-resume", "-name", "-from"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing-flags error should mention %s, got: %v", want, err)
		}
	}
}

func TestParseFlags_PartiallyMissing(t *testing.T) {
	_, err := parseFlags([]string{"-domain", "acme.com", "-jd", "text"})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "-domain") || strings.Contains(err.Error(), "-jd") {
		t.Errorf("error should not list flags that were supplied: %v", err)
	}
	if !strings.Contains(err.Error(), "-resume") || !strings.Contains(err.Error(), "-name") || !strings.Contains(err.Error(), "-from") {
		t.Errorf("error should list the still-missing flags: %v", err)
	}
}

func TestParseFlags_Version(t *testing.T) {
	_, err := parseFlags([]string{"-version"})
	if !errors.Is(err, errVersionRequested) {
		t.Errorf("expected errVersionRequested, got: %v", err)
	}
}

func TestParseFlags_VersionBypassesRequiredFlags(t *testing.T) {
	// -version must work standalone, with none of the otherwise-required flags.
	_, err := parseFlags([]string{"-version"})
	if !errors.Is(err, errVersionRequested) {
		t.Errorf("expected errVersionRequested even with no other flags, got: %v", err)
	}
}

func TestParseFlags_Help(t *testing.T) {
	_, err := parseFlags([]string{"-h"})
	if !errors.Is(err, flag.ErrHelp) {
		t.Errorf("expected flag.ErrHelp, got: %v", err)
	}
}

func TestParseFlags_UnknownFlag(t *testing.T) {
	_, err := parseFlags([]string{"-bogus"})
	if err == nil {
		t.Error("expected error for unknown flag")
	}
}

func TestParseFlags_Mock(t *testing.T) {
	args := append(validArgs(), "-mock")
	cfg, err := parseFlags(args)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if !cfg.useMock {
		t.Error("expected useMock = true")
	}
}

func contactFixture(first, last, email string, dept types.Department) types.Contact {
	return types.Contact{FirstName: first, LastName: last, Email: email, Department: dept}
}

func TestSelectContacts_BlankCancels(t *testing.T) {
	ordered := []types.Contact{contactFixture("Ada", "Lovelace", "ada@acme.com", types.DeptIT)}
	got, err := selectContacts(strings.NewReader("\n"), ordered, 1, types.DeptIT)
	if err != nil {
		t.Fatalf("selectContacts: %v", err)
	}
	if got != nil {
		t.Errorf("blank input should cancel (nil selection), got %v", got)
	}
}

func TestSelectContacts_AllMatched(t *testing.T) {
	ordered := []types.Contact{
		contactFixture("Ada", "Lovelace", "ada@acme.com", types.DeptIT),
		contactFixture("Grace", "Hopper", "grace@acme.com", types.DeptIT),
		contactFixture("Kay", "Finance", "kay@acme.com", types.DeptFinance),
	}
	got, err := selectContacts(strings.NewReader("a\n"), ordered, 2, types.DeptIT)
	if err != nil {
		t.Fatalf("selectContacts: %v", err)
	}
	if len(got) != 2 || got[0].Email != "ada@acme.com" || got[1].Email != "grace@acme.com" {
		t.Errorf("'a' should select all matched contacts, got %+v", got)
	}
}

func TestSelectContacts_IndividualSelectionWithDedup(t *testing.T) {
	ordered := []types.Contact{
		contactFixture("Ada", "Lovelace", "ada@acme.com", types.DeptIT),
		contactFixture("Grace", "Hopper", "grace@acme.com", types.DeptIT),
		contactFixture("Kay", "Finance", "kay@acme.com", types.DeptFinance),
	}
	got, err := selectContacts(strings.NewReader("1, 3, 1\n"), ordered, 2, types.DeptIT)
	if err != nil {
		t.Fatalf("selectContacts: %v", err)
	}
	if len(got) != 2 || got[0].Email != "ada@acme.com" || got[1].Email != "kay@acme.com" {
		t.Errorf("expected [ada, kay] with duplicate collapsed, got %+v", got)
	}
}

func TestSelectContacts_OutOfRangeRejected(t *testing.T) {
	ordered := []types.Contact{contactFixture("Ada", "Lovelace", "ada@acme.com", types.DeptIT)}
	_, err := selectContacts(strings.NewReader("5\n"), ordered, 1, types.DeptIT)
	if err == nil {
		t.Error("expected out-of-range rejection")
	}
}

func TestSelectContacts_MalformedTokenRejected(t *testing.T) {
	ordered := []types.Contact{contactFixture("Ada", "Lovelace", "ada@acme.com", types.DeptIT)}
	// A trailing non-digit must not be silently truncated into a valid number.
	_, err := selectContacts(strings.NewReader("1x\n"), ordered, 1, types.DeptIT)
	if err == nil {
		t.Error("expected rejection of malformed token \"1x\"")
	}
}

func TestSelectContacts_NoTrailingNewlineStillWorks(t *testing.T) {
	ordered := []types.Contact{contactFixture("Ada", "Lovelace", "ada@acme.com", types.DeptIT)}
	got, err := selectContacts(strings.NewReader("1"), ordered, 1, types.DeptIT)
	if err != nil {
		t.Fatalf("selectContacts: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 selected contact, got %+v", got)
	}
}

func TestContactLine_SanitizesControlBytes(t *testing.T) {
	c := contactFixture("Evil\x1b[31m", "Name", "evil@acme.com", types.DeptIT)
	c.Position = "Boss\r\nX-Injected: yes"
	line := contactLine(c)
	if strings.ContainsAny(line, "\r\n\x1b") {
		t.Errorf("contactLine should strip control bytes, got %q", line)
	}
}

func TestContactLabel(t *testing.T) {
	named := contactFixture("Ada", "Lovelace", "ada@acme.com", types.DeptIT)
	if got := contactLabel(named); got != "Ada Lovelace" {
		t.Errorf("contactLabel(named) = %q", got)
	}
	unnamed := types.Contact{Email: "info@acme.com"}
	if got := contactLabel(unnamed); got != "info@acme.com" {
		t.Errorf("contactLabel(unnamed) = %q", got)
	}
}

func TestEmlBaseName(t *testing.T) {
	cases := []struct {
		c    types.Contact
		want string
	}{
		{contactFixture("Ada", "Lovelace", "ada@acme.com", types.DeptIT), "ada-lovelace"},
		{types.Contact{Email: "info@acme.com"}, "info-acme-com"},
		{types.Contact{}, "contact"},
	}
	for _, tc := range cases {
		if got := emlBaseName(tc.c); got != tc.want {
			t.Errorf("emlBaseName(%+v) = %q, want %q", tc.c, got, tc.want)
		}
	}
}

func TestUniqueEmlFilename_DedupesCollisions(t *testing.T) {
	used := map[string]int{}
	// Two different contacts that sanitize to the same base name.
	a := contactFixture("Info", "", "a@acme.com", types.DeptUnknown)
	b := contactFixture("Info", "", "b@acme.com", types.DeptUnknown)
	c := contactFixture("Info", "", "c@acme.com", types.DeptUnknown)

	f1 := uniqueEmlFilename(a, used)
	f2 := uniqueEmlFilename(b, used)
	f3 := uniqueEmlFilename(c, used)

	if f1 == f2 || f2 == f3 || f1 == f3 {
		t.Errorf("colliding contacts should produce distinct filenames, got %q, %q, %q", f1, f2, f3)
	}
	if f1 != "info.eml" {
		t.Errorf("first occurrence should keep the plain name, got %q", f1)
	}
}

func TestUniqueEmlFilename_TwoNamelessContactsBothFallBackToContact(t *testing.T) {
	used := map[string]int{}
	f1 := uniqueEmlFilename(types.Contact{}, used)
	f2 := uniqueEmlFilename(types.Contact{}, used)
	if f1 == f2 {
		t.Errorf("two nameless contacts must not collide on disk, both got %q", f1)
	}
}

func TestResumeTextForGrounding_PlainText(t *testing.T) {
	got := resumeTextForGrounding("resume.txt", []byte("Jane Doe, Software Engineer"))
	if got != "Jane Doe, Software Engineer" {
		t.Errorf("plain text resume should pass through verbatim, got %q", got)
	}
}

func TestResumeTextForGrounding_BinaryFallsBackToNote(t *testing.T) {
	got := resumeTextForGrounding("resume.pdf", []byte{0x25, 0x50, 0x44, 0x46, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05})
	if !strings.Contains(got, ".pdf") {
		t.Errorf("binary resume should fall back to a note naming the extension, got %q", got)
	}
}

func TestResumeTextForGrounding_TextualNonTxtExtensionUsesContent(t *testing.T) {
	got := resumeTextForGrounding("resume.rtf", []byte("Jane Doe, plain readable text"))
	if got != "Jane Doe, plain readable text" {
		t.Errorf("mostly-textual content should be used directly regardless of extension, got %q", got)
	}
}

func TestLooksTextual(t *testing.T) {
	if looksTextual(nil) {
		t.Error("empty data should not look textual")
	}
	if !looksTextual([]byte("plain ascii text with some\nnewlines\tand tabs")) {
		t.Error("plain ASCII should look textual")
	}
	if looksTextual([]byte{0x00, 0x01, 0x02, 'a', 'b', 'c'}) {
		t.Error("data containing a NUL byte should never look textual")
	}
	mostlyControl := make([]byte, 100)
	for i := range mostlyControl {
		mostlyControl[i] = 0x01 // nonprintable, but not NUL, to exercise the ratio check
	}
	if looksTextual(mostlyControl) {
		t.Error("mostly-control-byte data should not look textual")
	}
}
