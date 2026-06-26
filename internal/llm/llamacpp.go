package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// LlamaCpp is a Completer backed by a local llama.cpp server, using the native
// /completion endpoint (not the OpenAI-compatible /v1/chat/completions one).
//
// The native endpoint is chosen deliberately for option B: it accepts a raw
// GBNF grammar string in the `grammar` field, so the extraction node's grammar
// is hand-authored and sent verbatim, with no JSON-schema-to-grammar conversion
// in the path and no response_format envelope. The cost is that this endpoint
// is llama.cpp-specific rather than portable — acceptable here because the
// default backend IS llama.cpp and cross-provider portability is explicitly a
// router's concern, not grease's.
//
// The design target is a CPU-only workstation running a non-thinking instruct
// model (e.g. Qwen2.5-7B-Instruct, Llama 3.1-8B). Thinking models fit poorly:
// hidden reasoning tokens inflate time-to-first-token unpredictably in an
// interactive tool.
//
// Prompt framing: the native endpoint takes a single prompt string, so the
// caller is responsible for any chat-template wrapping the model expects. grease
// assembles the full prompt before calling; this adapter sends it as-is.
type LlamaCpp struct {
	// BaseURL is the server root, e.g. "http://localhost:8080".
	BaseURL string
	// HTTP is the client used for requests. If nil, a client with a generous
	// timeout is used (CPU inference is slow).
	HTTP *http.Client
}

// NewLlamaCpp builds an adapter with a default HTTP client. baseURL is required.
func NewLlamaCpp(baseURL string) *LlamaCpp {
	return &LlamaCpp{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP:    &http.Client{Timeout: 5 * time.Minute},
	}
}

// completionRequest mirrors the subset of llama.cpp's /completion request body
// that grease uses. Field names match the server's native API exactly.
type completionRequest struct {
	Prompt      string   `json:"prompt"`
	Temperature float64  `json:"temperature"`
	NPredict    int      `json:"n_predict,omitempty"`
	Stop        []string `json:"stop,omitempty"`
	Grammar     string   `json:"grammar,omitempty"`
	Stream      bool     `json:"stream"`
	CachePrompt bool     `json:"cache_prompt"`
}

// completionResponse mirrors the /completion result. Unlike the chat endpoint,
// the generated text is at the top level in `content`, not nested under choices.
type completionResponse struct {
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
	Model     string `json:"model"`
	// Errors (e.g. unparseable grammar) come back in this object.
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    int    `json:"code"`
	} `json:"error"`
}

// Complete implements unconstrained generation.
func (l *LlamaCpp) Complete(ctx context.Context, req Request) (string, error) {
	return l.do(ctx, req, "")
}

// CompleteConstrained implements grammar-constrained generation. An empty
// grammar is a programming error — callers wanting unconstrained output must
// use Complete — so it is rejected rather than silently behaving like Complete.
func (l *LlamaCpp) CompleteConstrained(ctx context.Context, req Request, grammar string) (string, error) {
	if strings.TrimSpace(grammar) == "" {
		return "", fmt.Errorf("llamacpp: CompleteConstrained called with empty grammar")
	}
	return l.do(ctx, req, grammar)
}

func (l *LlamaCpp) do(ctx context.Context, req Request, grammar string) (string, error) {
	if l.BaseURL == "" {
		return "", fmt.Errorf("llamacpp: BaseURL is empty")
	}

	body := completionRequest{
		Prompt:      req.Prompt,
		Temperature: req.Temperature,
		NPredict:    req.MaxTokens,
		Stop:        req.Stop,
		Grammar:     grammar,
		Stream:      false,
		// cache_prompt can produce nondeterministic logits across batch sizes;
		// for a tool whose whole thesis is determinism, leave it off so repeated
		// runs over the same prompt behave consistently.
		CachePrompt: false,
	}

	buf, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("llamacpp: marshal request: %w", err)
	}

	url := l.BaseURL + "/completion"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return "", fmt.Errorf("llamacpp: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := l.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("llamacpp: request to %s: %w", url, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("llamacpp: read response: %w", err)
	}

	var parsed completionResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("llamacpp: status %d, unparseable body: %s",
			resp.StatusCode, snippet(raw))
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if parsed.Error != nil && parsed.Error.Message != "" {
			return "", fmt.Errorf("llamacpp: status %d: %s",
				resp.StatusCode, parsed.Error.Message)
		}
		return "", fmt.Errorf("llamacpp: status %d: %s",
			resp.StatusCode, snippet(raw))
	}

	// A 200 can still carry an error object in some builds; treat a populated
	// error as failure regardless of status.
	if parsed.Error != nil && parsed.Error.Message != "" {
		return "", fmt.Errorf("llamacpp: %s", parsed.Error.Message)
	}

	// Truncation means the context overflowed and output is incomplete. For a
	// constrained extraction this would likely produce invalid JSON anyway, but
	// surfacing it explicitly gives a clearer failure than a downstream parse
	// error. The caller's validation will also catch it; this just names the
	// cause.
	if parsed.Truncated {
		return parsed.Content, fmt.Errorf("llamacpp: generation truncated (context overflow); output incomplete")
	}

	return parsed.Content, nil
}

// snippet bounds an arbitrary response body for inclusion in an error.
func snippet(b []byte) string {
	const max = 300
	s := strings.TrimSpace(string(b))
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
