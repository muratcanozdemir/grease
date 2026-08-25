// Package emit assembles a ready-to-send email as an RFC 5322 .eml file with
// the resume attached.
//
// This is fully deterministic: given the same draft body, recipient, subject,
// and resume, it produces byte-identical output. No model, no randomness beyond
// a MIME boundary (which is derived, not random, for reproducibility). The
// result is a standard .eml the user opens in their own mail client, reviews,
// and sends themselves — grease does not send anything, so there is no OAuth,
// no token storage, and no "did it actually go out" ambiguity.
//
// Everything here is stdlib. An .eml with one text part and one attachment is a
// MIME multipart/mixed document; mime/multipart and encoding/base64 cover it
// without a third-party mail library.
package emit

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"mime"
	"mime/multipart"
	"net/textproto"
	"path/filepath"
	"strings"
	"time"
)

// Email is everything needed to assemble one .eml.
type Email struct {
	FromName    string
	FromAddress string
	ToName      string
	ToAddress   string
	Subject     string
	Body        string // plain-text body (the drafted email)
	Date        time.Time

	// Resume attachment.
	ResumeFilename string // e.g. "jane-doe-cv.pdf"
	ResumeBytes    []byte
	// ResumeMIMEType is the attachment's content type. If empty, it is guessed
	// from the filename extension, defaulting to application/octet-stream.
	ResumeMIMEType string
}

// Build produces the complete .eml bytes.
func Build(e Email) ([]byte, error) {
	if e.ToAddress == "" {
		return nil, fmt.Errorf("emit: missing recipient address")
	}
	if strings.TrimSpace(e.Body) == "" {
		return nil, fmt.Errorf("emit: empty body")
	}
	// Header values are otherwise safe: Subject and the From/To display names go
	// through mime.QEncoding, which escapes control bytes as part of encoding
	// non-ASCII. The raw addresses do not pass through an encoder anywhere, so
	// they are the one place a CRLF could smuggle extra headers (e.g. a bcc)
	// into the message — checked explicitly here rather than trusted from an
	// external source (the recipient address in particular comes from the
	// enrichment provider, not from grease itself).
	if err := checkHeaderSafe("From address", e.FromAddress); err != nil {
		return nil, err
	}
	if err := checkHeaderSafe("To address", e.ToAddress); err != nil {
		return nil, err
	}

	date := e.Date
	if date.IsZero() {
		date = time.Now()
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	// A deterministic boundary: derived from the content so identical input
	// yields identical output. Mail clients only require it be unique within the
	// message and absent from the parts; a content hash satisfies both.
	boundary := deterministicBoundary(e)
	if err := mw.SetBoundary(boundary); err != nil {
		return nil, fmt.Errorf("emit: set boundary: %w", err)
	}

	// Top-level headers. Written before the multipart body. RFC 5322 addresses
	// with display names are encoded via mime.QEncoding for any non-ASCII.
	var hdr bytes.Buffer
	writeHeader(&hdr, "From", formatAddress(e.FromName, e.FromAddress))
	writeHeader(&hdr, "To", formatAddress(e.ToName, e.ToAddress))
	writeHeader(&hdr, "Subject", mime.QEncoding.Encode("utf-8", e.Subject))
	writeHeader(&hdr, "Date", date.Format(time.RFC1123Z))
	writeHeader(&hdr, "MIME-Version", "1.0")
	writeHeader(&hdr, "Content-Type", fmt.Sprintf("multipart/mixed; boundary=%q", boundary))
	hdr.WriteString("\r\n") // blank line separating headers from body

	// Part 1: the text body.
	textHeader := textproto.MIMEHeader{}
	textHeader.Set("Content-Type", "text/plain; charset=utf-8")
	textHeader.Set("Content-Transfer-Encoding", "quoted-printable")
	tw, err := mw.CreatePart(textHeader)
	if err != nil {
		return nil, fmt.Errorf("emit: create text part: %w", err)
	}
	if _, err := tw.Write(quotedPrintable(e.Body)); err != nil {
		return nil, fmt.Errorf("emit: write text part: %w", err)
	}

	// Part 2: the resume attachment, if present.
	if len(e.ResumeBytes) > 0 {
		filename := e.ResumeFilename
		if filename == "" {
			filename = "resume"
		}
		ctype := e.ResumeMIMEType
		if ctype == "" {
			ctype = guessMIME(filename)
		}
		attHeader := textproto.MIMEHeader{}
		attHeader.Set("Content-Type", fmt.Sprintf("%s; name=%q", ctype, filename))
		attHeader.Set("Content-Transfer-Encoding", "base64")
		attHeader.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
		aw, err := mw.CreatePart(attHeader)
		if err != nil {
			return nil, fmt.Errorf("emit: create attachment part: %w", err)
		}
		if _, err := aw.Write(base64Wrapped(e.ResumeBytes)); err != nil {
			return nil, fmt.Errorf("emit: write attachment: %w", err)
		}
	}

	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("emit: close multipart: %w", err)
	}

	return append(hdr.Bytes(), buf.Bytes()...), nil
}

// checkHeaderSafe rejects a raw header value (one that will not pass through
// an encoder before being written) that contains a CR or LF. Either would let
// the value break out of its header line and inject arbitrary additional
// headers into the message.
func checkHeaderSafe(field, value string) error {
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("emit: %s contains a line break, which would inject headers into the message", field)
	}
	return nil
}

func writeHeader(b *bytes.Buffer, key, value string) {
	b.WriteString(key)
	b.WriteString(": ")
	b.WriteString(value)
	b.WriteString("\r\n")
}

// formatAddress renders a "Display Name <addr>" header value, encoding the
// display name if it contains non-ASCII.
func formatAddress(name, addr string) string {
	if name == "" {
		return addr
	}
	return fmt.Sprintf("%s <%s>", mime.QEncoding.Encode("utf-8", name), addr)
}

// deterministicBoundary derives a stable boundary from the message content so
// the output is reproducible. The prefix keeps it recognizable; the hash keeps
// it unique to this message and vanishingly unlikely to collide with body text.
func deterministicBoundary(e Email) string {
	h := sha256.New()
	h.Write([]byte(e.FromAddress))
	h.Write([]byte(e.ToAddress))
	h.Write([]byte(e.Subject))
	h.Write([]byte(e.Body))
	h.Write(e.ResumeBytes)
	return "grease-" + fmt.Sprintf("%x", h.Sum(nil))[:32]
}

// base64Wrapped encodes bytes to base64 with lines wrapped at 76 chars per MIME.
func base64Wrapped(data []byte) []byte {
	encoded := base64.StdEncoding.EncodeToString(data)
	const lineLen = 76
	var out bytes.Buffer
	for i := 0; i < len(encoded); i += lineLen {
		end := i + lineLen
		if end > len(encoded) {
			end = len(encoded)
		}
		out.WriteString(encoded[i:end])
		out.WriteString("\r\n")
	}
	return out.Bytes()
}

func guessMIME(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	switch ext {
	case ".pdf":
		return "application/pdf"
	case ".doc":
		return "application/msword"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".txt", ".md":
		return "text/plain"
	default:
		return "application/octet-stream"
	}
}
