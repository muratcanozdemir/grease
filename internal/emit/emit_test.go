package emit

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
	"time"
)

func sampleEmail() Email {
	return Email{
		FromName:       "Jane Doe",
		FromAddress:    "jane@example.com",
		ToName:         "Ada Lovelace",
		ToAddress:      "ada.lovelace@acme.com",
		Subject:        "Re: Senior Platform Engineer — Jane Doe",
		Body:           "Hi Ada,\n\nI'm reaching out about the Platform Engineer role. I've run production Kubernetes and written Go controllers.\n\nCould we talk briefly?\n\nBest,\nJane",
		Date:           time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC),
		ResumeFilename: "jane-doe-cv.pdf",
		ResumeBytes:    []byte("%PDF-1.4 fake pdf bytes \x00\x01\x02 binary"),
		ResumeMIMEType: "application/pdf",
	}
}

// TestBuild_ParsesAsValidMIME is the load-bearing test: it builds the .eml and
// then parses it back the way a mail client would, asserting the structure is
// well-formed and the parts round-trip.
func TestBuild_ParsesAsValidMIME(t *testing.T) {
	raw, err := Build(sampleEmail())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("output is not a parseable RFC 5322 message: %v", err)
	}

	// Headers.
	if got := msg.Header.Get("MIME-Version"); got != "1.0" {
		t.Errorf("MIME-Version = %q", got)
	}
	if from := msg.Header.Get("From"); !strings.Contains(from, "jane@example.com") {
		t.Errorf("From missing address: %q", from)
	}
	// Subject decodes (it's ASCII here, so QEncoding passes it through).
	dec := new(mime.WordDecoder)
	subj, err := dec.DecodeHeader(msg.Header.Get("Subject"))
	if err != nil {
		t.Fatalf("decode subject: %v", err)
	}
	if !strings.Contains(subj, "Senior Platform Engineer") {
		t.Errorf("subject = %q", subj)
	}

	// Content-Type must be multipart/mixed with a boundary.
	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("parse Content-Type: %v", err)
	}
	if mediaType != "multipart/mixed" {
		t.Errorf("media type = %q, want multipart/mixed", mediaType)
	}
	boundary := params["boundary"]
	if boundary == "" {
		t.Fatal("no boundary param")
	}

	// Walk the parts.
	mr := multipart.NewReader(msg.Body, boundary)
	var sawText, sawAttachment bool
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("reading parts: %v", err)
		}
		ct := part.Header.Get("Content-Type")
		switch {
		case strings.HasPrefix(ct, "text/plain"):
			sawText = true
			// Body part is quoted-printable; mime/multipart does NOT auto-decode
			// CTE, so decode by reading raw and checking the QP didn't mangle
			// readable ASCII (which QP leaves intact).
			b, _ := io.ReadAll(part)
			if !strings.Contains(string(b), "Platform Engineer role") {
				t.Errorf("text part lost content: %q", string(b))
			}
		case strings.HasPrefix(ct, "application/pdf"):
			sawAttachment = true
			if cte := part.Header.Get("Content-Transfer-Encoding"); cte != "base64" {
				t.Errorf("attachment CTE = %q, want base64", cte)
			}
			disp := part.Header.Get("Content-Disposition")
			if !strings.Contains(disp, "attachment") || !strings.Contains(disp, "jane-doe-cv.pdf") {
				t.Errorf("attachment disposition = %q", disp)
			}
			// Decode base64 and confirm the bytes round-trip exactly.
			b, _ := io.ReadAll(part)
			decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(strings.ReplaceAll(string(b), "\r\n", ""), "\n", ""))
			if err != nil {
				t.Fatalf("attachment base64 invalid: %v", err)
			}
			if !bytes.Equal(decoded, sampleEmail().ResumeBytes) {
				t.Errorf("attachment bytes did not round-trip: got %q", decoded)
			}
		}
	}
	if !sawText {
		t.Error("no text part found")
	}
	if !sawAttachment {
		t.Error("no attachment part found")
	}
}

func TestBuild_Deterministic(t *testing.T) {
	a, err := Build(sampleEmail())
	if err != nil {
		t.Fatal(err)
	}
	b, err := Build(sampleEmail())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Error("Build is not deterministic for identical input")
	}
}

func TestBuild_NoAttachment(t *testing.T) {
	e := sampleEmail()
	e.ResumeBytes = nil
	raw, err := Build(e)
	if err != nil {
		t.Fatalf("Build without attachment: %v", err)
	}
	// Should still be valid and contain the text part.
	if _, err := mail.ReadMessage(bytes.NewReader(raw)); err != nil {
		t.Errorf("no-attachment email not parseable: %v", err)
	}
}

func TestBuild_RejectsEmptyBodyAndRecipient(t *testing.T) {
	e := sampleEmail()
	e.Body = "  "
	if _, err := Build(e); err == nil {
		t.Error("expected empty-body rejection")
	}
	e = sampleEmail()
	e.ToAddress = ""
	if _, err := Build(e); err == nil {
		t.Error("expected empty-recipient rejection")
	}
}

func TestBuild_NonASCIISubjectEncoded(t *testing.T) {
	e := sampleEmail()
	e.Subject = "Über die Platform-Rolle — Café"
	raw, err := Build(e)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	dec := new(mime.WordDecoder)
	subj, err := dec.DecodeHeader(msg.Header.Get("Subject"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if subj != e.Subject {
		t.Errorf("non-ASCII subject did not round-trip: got %q want %q", subj, e.Subject)
	}
}

func TestGuessMIME(t *testing.T) {
	cases := map[string]string{
		"cv.pdf":    "application/pdf",
		"resume.txt": "text/plain",
	}
	for name, wantPrefix := range cases {
		got := guessMIME(name)
		if !strings.HasPrefix(got, strings.Split(wantPrefix, ";")[0]) {
			t.Errorf("guessMIME(%q) = %q, want prefix %q", name, got, wantPrefix)
		}
	}
	if got := guessMIME("noext"); got != "application/octet-stream" {
		t.Errorf("guessMIME(noext) = %q", got)
	}
}
