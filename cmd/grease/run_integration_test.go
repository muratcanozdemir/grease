package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeLlamaCppServer mimics enough of llama.cpp's /completion endpoint to
// drive both LLM nodes: a request carrying a "grammar" field is the
// extraction call and gets back a fixed, valid Extraction JSON payload;
// anything else is the drafting call and gets back a short prose body.
func fakeLlamaCppServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("fake server: request body not valid JSON: %v", err)
		}

		var content string
		if g, ok := body["grammar"].(string); ok && g != "" {
			content = `{"company":"Acme Payments","role":"Platform Engineer","tech_stack":["Go","Kubernetes"],"seniority":"senior","department":"it"}`
		} else {
			content = "Hi,\n\nI would love to talk about the Platform Engineer role.\n\nBest,\nApplicant"
		}

		resp, _ := json.Marshal(map[string]any{"content": content})
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(resp)
	}))
}

// TestRun_HappyPathEndToEnd drives the whole pipeline — the exact path split
// across two context timeouts (lookup phase, then interactive selection, then
// draft phase) — through a fake LLM backend and the mock contact provider,
// and checks it lands the expected .eml files on disk without colliding
// filenames.
func TestRun_HappyPathEndToEnd(t *testing.T) {
	llmSrv := fakeLlamaCppServer(t)
	defer llmSrv.Close()

	tmp := t.TempDir()
	resumePath := filepath.Join(tmp, "resume.txt")
	if err := os.WriteFile(resumePath, []byte("Jane Doe — Platform Engineer, 8 years Go and Kubernetes."), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(tmp, "out")

	oldArgs := os.Args
	os.Args = []string{
		"grease",
		"-domain", "acme.com",
		"-jd", "Senior Platform Engineer, Go and Kubernetes required.",
		"-resume", resumePath,
		"-name", "Jane Doe",
		"-from", "jane@example.com",
		"-out", outDir,
		"-llm", llmSrv.URL,
		"-mock",
	}
	defer func() { os.Args = oldArgs }()

	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()
	if _, err := w.WriteString("a\n"); err != nil {
		t.Fatal(err)
	}
	w.Close()

	if err := run(); err != nil {
		t.Fatalf("run(): %v", err)
	}

	// SampleResult's IT contacts (ada, grace, alan) are the department-matched
	// set for an extraction that resolves to "it"; "a" selects all of them.
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("reading out dir: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	wantAny := []string{"ada-lovelace.eml", "grace-hopper.eml", "alan-turing.eml"}
	if len(names) != len(wantAny) {
		t.Fatalf("wrote %d files (%v), want %d", len(names), names, len(wantAny))
	}
	for _, want := range wantAny {
		found := false
		for _, got := range names {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected output file %q, got %v", want, names)
		}
	}

	// Each file should be a parseable .eml with the drafted body and the
	// resume attached (spot-check one).
	raw, err := os.ReadFile(filepath.Join(outDir, "ada-lovelace.eml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "To: Ada Lovelace <ada.lovelace@acme.com>") {
		t.Errorf("eml missing expected To header: %s", raw)
	}
	if !strings.Contains(string(raw), "Platform Engineer role") {
		t.Errorf("eml missing drafted body: %s", raw)
	}
}
