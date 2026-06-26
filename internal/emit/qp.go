package emit

import (
	"bytes"
	"mime/quotedprintable"
)

// quotedPrintable encodes text using the standard quoted-printable transfer
// encoding, which keeps ASCII readable while safely encoding any non-ASCII or
// control bytes in the body. Using the stdlib encoder rather than hand-rolling
// it avoids the subtle line-length and soft-break rules QP requires.
func quotedPrintable(s string) []byte {
	var buf bytes.Buffer
	w := quotedprintable.NewWriter(&buf)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return buf.Bytes()
}
