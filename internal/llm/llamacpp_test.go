package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func captureServer(t *testing.T, status int, replyBody string, captured *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/completion" {
			t.Errorf("unexpected path: %s, want /completion", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		raw, _ := io.ReadAll(r.Body)
		if captured != nil {
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Errorf("request body not valid JSON: %v", err)
			}
			*captured = m
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, replyBody)
	}))
}

func okBody(content string) string {
	b, _ := json.Marshal(completionResponse{Content: content})
	return string(b)
}

func TestComplete_Unconstrained(t *testing.T) {
	var got map[string]any
	srv := captureServer(t, http.StatusOK, okBody("hello there"), &got)
	defer srv.Close()

	c := NewLlamaCpp(srv.URL)
	out, err := c.Complete(context.Background(), Request{
		Prompt:      "hi",
		Temperature: 0.7,
		MaxTokens:   128,
		Stop:        []string{"\n\n"},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if out != "hello there" {
		t.Errorf("output = %q, want %q", out, "hello there")
	}
	// Unconstrained: no grammar field should be present.
	if _, present := got["grammar"]; present {
		t.Errorf("Complete must not send a grammar field, got: %v", got["grammar"])
	}
	if got["prompt"] != "hi" {
		t.Errorf("prompt = %v, want hi", got["prompt"])
	}
	if got["n_predict"] != float64(128) {
		t.Errorf("n_predict = %v, want 128", got["n_predict"])
	}
	if got["stream"] != false {
		t.Errorf("stream = %v, want false", got["stream"])
	}
	// Determinism: cache_prompt must be off.
	if got["cache_prompt"] != false {
		t.Errorf("cache_prompt = %v, want false", got["cache_prompt"])
	}
}

func TestCompleteConstrained_SendsGrammar(t *testing.T) {
	var got map[string]any
	srv := captureServer(t, http.StatusOK, okBody(`{"x":1}`), &got)
	defer srv.Close()

	grammar := `root ::= "{" "\"x\":" [0-9]+ "}"`
	c := NewLlamaCpp(srv.URL)
	_, err := c.CompleteConstrained(context.Background(), Request{Prompt: "go"}, grammar)
	if err != nil {
		t.Fatalf("CompleteConstrained: %v", err)
	}
	if got["grammar"] != grammar {
		t.Errorf("grammar = %v, want the supplied GBNF", got["grammar"])
	}
}

func TestCompleteConstrained_EmptyGrammarRejected(t *testing.T) {
	// Must fail without even hitting the network — empty grammar is a caller bug.
	c := NewLlamaCpp("http://127.0.0.1:1") // unreachable on purpose
	_, err := c.CompleteConstrained(context.Background(), Request{Prompt: "x"}, "   ")
	if err == nil || !strings.Contains(err.Error(), "empty grammar") {
		t.Errorf("expected empty-grammar error, got: %v", err)
	}
}

func TestComplete_ErrorStatusSurfacesBackendMessage(t *testing.T) {
	errBody := `{"error":{"code":400,"message":"Failed to parse grammar","type":"invalid_request_error"}}`
	srv := captureServer(t, http.StatusBadRequest, errBody, nil)
	defer srv.Close()

	c := NewLlamaCpp(srv.URL)
	_, err := c.CompleteConstrained(context.Background(), Request{Prompt: "x"}, "root ::= bad")
	if err == nil || !strings.Contains(err.Error(), "Failed to parse grammar") {
		t.Errorf("expected grammar parse error surfaced, got: %v", err)
	}
}

func TestComplete_TruncationIsAnError(t *testing.T) {
	b, _ := json.Marshal(completionResponse{Content: `{"partial":`, Truncated: true})
	srv := captureServer(t, http.StatusOK, string(b), nil)
	defer srv.Close()

	c := NewLlamaCpp(srv.URL)
	out, err := c.Complete(context.Background(), Request{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "truncated") {
		t.Errorf("expected truncation error, got err=%v", err)
	}
	// Content is still returned alongside the error for diagnosis.
	if out != `{"partial":` {
		t.Errorf("expected partial content returned, got %q", out)
	}
}

func TestComplete_EmptyBaseURL(t *testing.T) {
	c := &LlamaCpp{}
	_, err := c.Complete(context.Background(), Request{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "BaseURL") {
		t.Errorf("expected BaseURL error, got: %v", err)
	}
}

// Compile-time assertion that LlamaCpp satisfies the interface.
var _ Completer = (*LlamaCpp)(nil)
