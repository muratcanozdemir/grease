package jd

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSource_RawTextPassthrough(t *testing.T) {
	in := "We are hiring a Senior Platform Engineer. Go, Terraform, AWS."
	out, err := Source(context.Background(), in, nil)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	if out != in {
		t.Errorf("raw text should pass through unchanged, got %q", out)
	}
}

func TestSource_EmptyRejected(t *testing.T) {
	if _, err := Source(context.Background(), "   ", nil); err == nil {
		t.Error("expected empty-input rejection")
	}
}

func TestSource_FetchesAndStripsURL(t *testing.T) {
	html := `<html><head><style>.x{color:red}</style><script>alert(1)</script></head>
	<body><h1>Senior Platform Engineer</h1><p>We use <b>Go</b> &amp; Terraform.</p>
	<ul><li>AWS</li><li>Kubernetes</li></ul></body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ua := r.Header.Get("User-Agent"); ua == "" {
			t.Error("expected a User-Agent header")
		}
		_, _ = io.WriteString(w, html)
	}))
	defer srv.Close()

	out, err := Source(context.Background(), srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("Source(URL): %v", err)
	}
	// Script/style content must be gone.
	if strings.Contains(out, "alert") || strings.Contains(out, "color:red") {
		t.Errorf("script/style not stripped: %q", out)
	}
	// Visible text must survive, with the entity decoded.
	for _, want := range []string{"Senior Platform Engineer", "Go", "Terraform", "AWS", "Kubernetes", "&"} {
		if !strings.Contains(out, want) {
			t.Errorf("stripped text missing %q; got: %q", want, out)
		}
	}
}

func TestSource_URLBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	if _, err := Source(context.Background(), srv.URL, srv.Client()); err == nil {
		t.Error("expected error on 404")
	}
}

func TestStripHTML_CollapsesAndStructures(t *testing.T) {
	in := `<div>Line one</div><div>Line two</div>`
	out := StripHTML(in)
	if !strings.Contains(out, "Line one") || !strings.Contains(out, "Line two") {
		t.Errorf("lost content: %q", out)
	}
	// Block tags should have produced a separation, not glued words.
	if strings.Contains(out, "oneLine") {
		t.Errorf("block tags not converted to breaks: %q", out)
	}
}

func TestStripHTML_EntitiesDecoded(t *testing.T) {
	out := StripHTML(`R&amp;D team, &quot;platform&quot; &mdash; remote`)
	for _, want := range []string{"R&D", `"platform"`, "—"} {
		if !strings.Contains(out, want) {
			t.Errorf("entity not decoded, want %q in %q", want, out)
		}
	}
}
