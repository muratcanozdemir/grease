// Package jd ingests a job description from either raw text or a URL.
//
// This is a deterministic front door. If the input is text, it is used as-is.
// If it is a URL, the page is fetched and reduced to readable text by stripping
// markup — no model involved in deciding what to fetch or how to read it. The
// LLM does not enter the picture until extraction; ingestion's only job is to
// produce a plain-text JD for that node to parse.
package jd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Source returns the job-description text for a given input, which may be raw
// text or an http(s) URL. The decision is made on the input's shape, not by
// asking a model.
func Source(ctx context.Context, input string, client *http.Client) (string, error) {
	in := strings.TrimSpace(input)
	if in == "" {
		return "", fmt.Errorf("jd: empty input")
	}
	if isURL(in) {
		return fetchAndStrip(ctx, in, client)
	}
	return in, nil
}

func isURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func fetchAndStrip(ctx context.Context, rawURL string, client *http.Client) (string, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("jd: build request: %w", err)
	}
	// A plain, honest UA. Some job boards reject empty UAs.
	req.Header.Set("User-Agent", "grease/1.0 (+https://example.invalid/grease)")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("jd: fetch %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("jd: fetching %s returned status %d", rawURL, resp.StatusCode)
	}

	// Bound the read; a JD page should be modest, and an unbounded read off an
	// arbitrary URL is a memory risk.
	const maxBytes = 4 << 20 // 4 MiB
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return "", fmt.Errorf("jd: read body: %w", err)
	}

	text := StripHTML(string(body))
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("jd: page at %s yielded no readable text after stripping markup", rawURL)
	}
	return text, nil
}

// Markup-stripping regexes. This is intentionally simple: drop script/style
// blocks, remove tags, collapse whitespace, decode a few common entities. It is
// not a full HTML parser — grease does not need DOM fidelity, only the readable
// text the model will parse. A heavier parser would be a dependency and a
// brittleness source for no gain here.
var (
	reScriptStyle = regexp.MustCompile(`(?is)<(script|style)\b[^>]*>.*?</(script|style)>`)
	reComment     = regexp.MustCompile(`(?s)<!--.*?-->`)
	reTag         = regexp.MustCompile(`(?s)<[^>]+>`)
	reWhitespace  = regexp.MustCompile(`[ \t\f\v]+`)
	reBlankLines  = regexp.MustCompile(`\n[ \t]*\n[ \t]*(\n[ \t]*)+`)
)

// StripHTML reduces an HTML document to readable plain text. Exported for
// testing and because callers may have already-fetched HTML.
func StripHTML(html string) string {
	s := reScriptStyle.ReplaceAllString(html, " ")
	s = reComment.ReplaceAllString(s, " ")
	// Turn block-ish tags into newlines before deleting all tags, so structure
	// becomes line breaks rather than runs of glued-together words.
	s = blockTagsToNewlines(s)
	s = reTag.ReplaceAllString(s, "")
	s = decodeEntities(s)
	s = reWhitespace.ReplaceAllString(s, " ")
	// Normalize line endings and collapse runs of blank lines to a single one.
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = reBlankLines.ReplaceAllString(s, "\n\n")
	// Trim trailing spaces on each line.
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimRight(ln, " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

var reBlockTag = regexp.MustCompile(`(?i)</?(p|div|br|li|ul|ol|tr|h[1-6]|section|article|header|footer|table)\b[^>]*>`)

func blockTagsToNewlines(s string) string {
	return reBlockTag.ReplaceAllString(s, "\n")
}

// decodeEntities handles the handful of HTML entities common in job text. Not
// exhaustive by design — the goal is readable text, not byte-perfect decoding.
func decodeEntities(s string) string {
	r := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&#39;", "'",
		"&apos;", "'",
		"&nbsp;", " ",
		"&mdash;", "—",
		"&ndash;", "–",
		"&hellip;", "…",
	)
	return r.Replace(s)
}
