// Command grease turns a company domain, a role/JD, and a resume into
// ready-to-send .eml files — one per contact you choose.
//
// The flow is the pipeline made concrete:
//
//	JD (text or URL) --[ingest]--> text
//	text             --[LLM: extract, grammar-constrained]--> typed struct
//	domain           --[enrich: Hunter or mock]--> contacts
//	contacts+struct  --[filter: deterministic]--> matched / other
//	you              --[select]--> chosen contacts
//	each chosen      --[LLM: draft, unconstrained]--> body
//	body+resume      --[emit: deterministic MIME]--> .eml on disk
//
// You open the .eml files in your own mail client, review, and send. grease
// sends nothing.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/user/grease/internal/buildinfo"
	"github.com/user/grease/internal/draft"
	"github.com/user/grease/internal/emit"
	"github.com/user/grease/internal/enrich"
	"github.com/user/grease/internal/extract"
	"github.com/user/grease/internal/filter"
	"github.com/user/grease/internal/jd"
	"github.com/user/grease/internal/llm"
	"github.com/user/grease/internal/types"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

type config struct {
	domain     string
	jdInput    string
	resumePath string
	senderName string
	senderAddr string
	outDir     string
	llmURL     string
	useMock    bool
}

func run() error {
	cfg, err := parseFlags()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Resume is read once, kept as bytes (for the attachment) and as text (for
	// grounding the draft). Plain-text resumes ground the model directly; for a
	// PDF, the bytes still attach, and the text used for grounding is whatever
	// the file decodes to (the user can supply a .txt alongside for best
	// results — documented in README).
	resumeBytes, err := os.ReadFile(cfg.resumePath)
	if err != nil {
		return fmt.Errorf("reading resume %s: %w", cfg.resumePath, err)
	}
	resumeText := resumeTextForGrounding(cfg.resumePath, resumeBytes)

	// 1. Ingest JD.
	jdText, err := jd.Source(ctx, cfg.jdInput, nil)
	if err != nil {
		return err
	}

	// 2. LLM provider (shared by extraction and drafting), behind the interface.
	var model llm.Completer = llm.NewLlamaCpp(cfg.llmURL)

	// 3. Extract (grammar-constrained).
	fmt.Fprintln(os.Stderr, "· extracting role details from the job description…")
	ext, err := extract.New(model).Extract(ctx, jdText)
	if err != nil {
		return err
	}
	printExtraction(ext)

	// 4. Enrich (Hunter or mock), behind the interface.
	provider, err := buildProvider(cfg)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "· looking up contacts at %s…\n", cfg.domain)
	res, err := provider.FindByDomain(ctx, cfg.domain)
	if err != nil {
		return err
	}
	if len(res.Contacts) == 0 {
		return fmt.Errorf("no contacts found for %s", cfg.domain)
	}

	// 5. Filter (deterministic partition).
	part := filter.ByDepartment(res.Contacts, ext.Department)

	// 6. Select (interactive).
	ordered := append(append([]types.Contact{}, part.Matched...), part.Other...)
	chosen, err := selectContacts(ordered, len(part.Matched), ext.Department)
	if err != nil {
		return err
	}
	if len(chosen) == 0 {
		fmt.Fprintln(os.Stderr, "nothing selected; exiting.")
		return nil
	}

	// 7. Draft + 8. Emit, per chosen contact.
	if err := os.MkdirAll(cfg.outDir, 0o755); err != nil {
		return fmt.Errorf("creating output dir: %w", err)
	}
	drafter := draft.New(model)
	resumeFilename := filepath.Base(cfg.resumePath)

	for i, c := range chosen {
		fmt.Fprintf(os.Stderr, "· drafting email %d/%d to %s…\n", i+1, len(chosen), contactLabel(c))
		in := draft.Input{
			Extraction: ext,
			Org:        res.Organization,
			Contact:    c,
			ResumeText: resumeText,
			SenderName: cfg.senderName,
		}
		body, err := drafter.Draft(ctx, in)
		if err != nil {
			return fmt.Errorf("drafting for %s: %w", c.Email, err)
		}

		eml, err := emit.Build(emit.Email{
			FromName:       cfg.senderName,
			FromAddress:    cfg.senderAddr,
			ToName:         c.FullName(),
			ToAddress:      c.Email,
			Subject:        draft.SuggestSubject(in),
			Body:           body,
			Date:           time.Now(),
			ResumeFilename: resumeFilename,
			ResumeBytes:    resumeBytes,
		})
		if err != nil {
			return fmt.Errorf("building eml for %s: %w", c.Email, err)
		}

		outPath := filepath.Join(cfg.outDir, emlFilename(c))
		if err := os.WriteFile(outPath, eml, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", outPath, err)
		}
		fmt.Printf("  wrote %s\n", outPath)
	}

	fmt.Fprintf(os.Stderr, "\nDone. %d .eml file(s) in %s — open them in your mail client to review and send.\n", len(chosen), cfg.outDir)
	return nil
}

func parseFlags() (config, error) {
	var cfg config
	showVersion := flag.Bool("version", false, "print version and build information, then exit")
	flag.StringVar(&cfg.domain, "domain", "", "company domain to find contacts at, e.g. acme.com (required)")
	flag.StringVar(&cfg.jdInput, "jd", "", "job description: raw text, or an http(s) URL to a posting (required)")
	flag.StringVar(&cfg.resumePath, "resume", "", "path to your resume file, attached to each email (required)")
	flag.StringVar(&cfg.senderName, "name", "", "your name, used to sign emails (required)")
	flag.StringVar(&cfg.senderAddr, "from", "", "your email address, used in the From header (required)")
	flag.StringVar(&cfg.outDir, "out", "./grease-out", "directory to write .eml files into")
	flag.StringVar(&cfg.llmURL, "llm", "http://localhost:8080", "base URL of the llama.cpp server")
	flag.BoolVar(&cfg.useMock, "mock", false, "use the mock contact provider (no Hunter key, no network, no credits)")
	flag.Parse()

	// -version short-circuits everything else: it must work without the
	// otherwise-required flags.
	if *showVersion {
		fmt.Println(buildinfo.String())
		os.Exit(0)
	}

	var missing []string
	if cfg.domain == "" {
		missing = append(missing, "-domain")
	}
	if cfg.jdInput == "" {
		missing = append(missing, "-jd")
	}
	if cfg.resumePath == "" {
		missing = append(missing, "-resume")
	}
	if cfg.senderName == "" {
		missing = append(missing, "-name")
	}
	if cfg.senderAddr == "" {
		missing = append(missing, "-from")
	}
	if len(missing) > 0 {
		return cfg, fmt.Errorf("missing required flags: %s\n\nrun with -h for usage", strings.Join(missing, ", "))
	}
	return cfg, nil
}

func buildProvider(cfg config) (enrich.EmailProvider, error) {
	if cfg.useMock {
		fmt.Fprintln(os.Stderr, "· using mock contact provider (no Hunter calls)")
		return &enrich.MockProvider{Result: enrich.SampleResult(cfg.domain)}, nil
	}
	return enrich.NewHunterFromEnv()
}

func printExtraction(ext types.Extraction) {
	fmt.Fprintf(os.Stderr, "  role:       %s\n", ext.Role)
	if ext.Company != "" {
		fmt.Fprintf(os.Stderr, "  company:    %s\n", ext.Company)
	}
	if len(ext.TechStack) > 0 {
		fmt.Fprintf(os.Stderr, "  tech stack: %s\n", strings.Join(ext.TechStack, ", "))
	}
	fmt.Fprintf(os.Stderr, "  department: %s\n", ext.Department)
}

// selectContacts renders the partitioned contacts and reads the user's choice.
// Matched contacts (department equals the extracted guess) are listed first,
// then a divider, then the rest — nothing hidden.
func selectContacts(ordered []types.Contact, matchedCount int, dept types.Department) ([]types.Contact, error) {
	fmt.Println()
	fmt.Println("Contacts found:")
	for i, c := range ordered {
		if i == 0 && matchedCount > 0 {
			fmt.Printf("  — matching department (%s) —\n", dept)
		}
		if i == matchedCount {
			if matchedCount > 0 {
				fmt.Println("  — other departments —")
			} else {
				fmt.Println("  — (no department match; showing all) —")
			}
		}
		fmt.Printf("  [%d] %s\n", i+1, contactLine(c))
	}
	fmt.Println()
	fmt.Print("Select contacts to email (comma-separated numbers, 'a' for all matched, or blank to cancel): ")

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return nil, fmt.Errorf("reading selection: %w", err)
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, nil
	}
	if line == "a" || line == "A" {
		return ordered[:matchedCount], nil
	}

	var chosen []types.Contact
	seen := map[int]bool{}
	for _, tok := range strings.Split(line, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(tok, "%d", &n); err != nil {
			return nil, fmt.Errorf("invalid selection %q", tok)
		}
		if n < 1 || n > len(ordered) {
			return nil, fmt.Errorf("selection %d out of range (1-%d)", n, len(ordered))
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		chosen = append(chosen, ordered[n-1])
	}
	return chosen, nil
}

func contactLine(c types.Contact) string {
	name := c.FullName()
	if name == "" {
		name = c.Email
	}
	parts := []string{name}
	if c.Position != "" {
		parts = append(parts, c.Position)
	}
	parts = append(parts, c.Email)
	if c.Department != "" && c.Department != types.DeptUnknown {
		parts = append(parts, fmt.Sprintf("[%s]", c.Department))
	}
	if c.Confidence > 0 {
		parts = append(parts, fmt.Sprintf("conf %d", c.Confidence))
	}
	return strings.Join(parts, " · ")
}

func contactLabel(c types.Contact) string {
	if n := c.FullName(); n != "" {
		return n
	}
	return c.Email
}

// emlFilename derives a filesystem-safe .eml name from a contact.
func emlFilename(c types.Contact) string {
	base := c.Email
	if n := c.FullName(); n != "" {
		base = n
	}
	base = strings.ToLower(base)
	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '@' || r == '.' || r == ' ' || r == '-' || r == '_':
			b.WriteRune('-')
		}
	}
	name := strings.Trim(b.String(), "-")
	if name == "" {
		name = "contact"
	}
	return name + ".eml"
}

// resumeTextForGrounding returns text to ground the draft. For text-like files
// it's the content; for binary (PDF, docx) the raw bytes are not useful text,
// so it returns a short note and relies on the user supplying a .txt for best
// grounding (documented). The attachment still carries the real file regardless.
func resumeTextForGrounding(path string, data []byte) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".txt", ".md", ".text", "":
		return string(data)
	default:
		// Best-effort: if it looks like mostly text, use it; else signal absence.
		if looksTextual(data) {
			return string(data)
		}
		return fmt.Sprintf("(Resume provided as %s; supply a .txt version via -resume for richer personalization.)", ext)
	}
}

func looksTextual(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	sample := data
	if len(sample) > 1024 {
		sample = sample[:1024]
	}
	nonPrintable := 0
	for _, b := range sample {
		if b == 0 {
			return false
		}
		if b < 9 || (b > 13 && b < 32) {
			nonPrintable++
		}
	}
	return nonPrintable*20 < len(sample)
}
