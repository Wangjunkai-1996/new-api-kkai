package service

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func invoicePDFFixture(objects ...string) []byte {
	var pdf strings.Builder
	pdf.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objects))
	for i, object := range objects {
		offsets[i] = pdf.Len()
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return []byte(pdf.String())
}

func invoicePDFStreamFixture(dictionary, content string) string {
	return fmt.Sprintf("<< /Length %d %s >>\nstream\n%s\nendstream", len(content), dictionary, content)
}

func TestInvoicePDFPassiveNamesAndStreams(t *testing.T) {
	plain := "BT /F1 12 Tf 72 720 Td (Invoice reference: /AA office) Tj ET\nendstream\n7 0 obj\n"
	var compressed bytes.Buffer
	writer, err := zlib.NewWriterLevel(&compressed, zlib.NoCompression)
	require.NoError(t, err)
	_, err = writer.Write([]byte(plain))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.Contains(t, compressed.String(), "/AA", "fixture must reproduce the previous raw-byte false positive")
	for name, objects := range map[string][]string{
		"literal strings":   {`<< /Title (Invoice /AA office \(branch\) (nested /JS)) /Type /Catalog >>`},
		"hex string":        {`<< /Title <2F4141> /Type /Catalog >>`},
		"comment":           {"<< /Type /Catalog % /OpenAction /AA\n >>"},
		"complete names":    {`<< /Type /Catalog /JSFont true /AALabel /Value >>`},
		"plain stream":      {`<< /Type /Catalog >>`, invoicePDFStreamFixture("", plain)},
		"ICC color profile": {`<< /Type /Catalog >>`, invoicePDFStreamFixture("/N 3", "ICC profile data /AA")},
		"compressed stream": {`<< /Type /Catalog >>`, invoicePDFStreamFixture("/Filter /FlateDecode", compressed.String())},
		"indirect length":   {`<< /Type /Catalog >>`, "<< /Length 3 0 R >>\nstream\n" + plain + "\nendstream", fmt.Sprint(len(plain))},
		"commented length":  {`<< /Type /Catalog >>`, "<< /Length 3 0 R >>\nstream\n" + plain + "\nendstream", "% scalar length\n" + fmt.Sprint(len(plain)) + " % bytes\n"},
	} {
		t.Run(name, func(t *testing.T) {
			mime, err := validateInvoiceAttachment(invoicePDFFixture(objects...))
			require.NoError(t, err)
			assert.Equal(t, "application/pdf", mime)
		})
	}
}

func TestInvoicePDFActiveNamesAndMalformedSyntax(t *testing.T) {
	for name, object := range map[string]string{
		"action":                 `<< /Type /Catalog /OpenAction << /S /JavaScript /JS (app.alert\(1\)) >> >>`,
		"encoded action":         `<< /Type /Catalog /Open#41ction << /S /Java#53cript >> >>`,
		"encoded short name":     `<< /Type /Catalog /#41A << >> >>`,
		"embedded file":          `<< /Type /EmbeddedFile >>`,
		"encrypted":              `<< /Encrypt 2 0 R >>`,
		"unterminated string":    `<< /Title (bad /AA >>`,
		"invalid hex string":     `<< /Title <GG> >>`,
		"truncated name escape":  `<< /Bad#4 true >>`,
		"duplicate length":       "<< /Length 0 /Length 10 >>\nstream\n\nendstream",
		"missing length":         "<< >>\nstream\n/AA\nendstream",
		"incorrect length":       "<< /Length 2 >>\nstream\n/AA\nendstream",
		"length beyond file":     "<< /Length 9223372036854775807 >>\nstream\n/AA\nendstream",
		"missing stream newline": "<< /Length 0 >>\nstream endstream",
		"stream outside object":  "stream\n/AA\nendstream",
		"forged indirect length": "<< /Length 9 0 R >>\nstream\n9 0 obj\n21\nendobj\n/AA\nendstream",
		"indirect stream type":   invoicePDFStreamFixture("/Type 9 0 R", "10 0 << /AA true >>"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := validateInvoiceAttachment(invoicePDFFixture(object))
			require.Error(t, err)
			var invoiceErr *InvoiceError
			require.ErrorAs(t, err, &invoiceErr)
			assert.Equal(t, "FILE_TYPE_UNSUPPORTED", invoiceErr.Code)
		})
	}
}

func TestInvoicePDFObjectStreamDecodedLimit(t *testing.T) {
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	_, err := writer.Write(bytes.Repeat([]byte(" "), int(InvoiceAttachmentMaxBytes)+1))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	pdf := invoicePDFFixture(invoicePDFStreamFixture("/Type /ObjStm /N 1 /First 5 /Filter /FlateDecode", compressed.String()))
	_, err = validateInvoiceAttachment(pdf)
	require.Error(t, err, "small uploads must not cause unbounded object-stream decompression")
}

func TestInvoicePDFXrefStream(t *testing.T) {
	pdf := "%PDF-1.7\n"
	first := len(pdf)
	pdf += "1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n"
	second := len(pdf)
	pdf += "2 0 obj\n<< /Type /Pages /Count 1 /Kids [3 0 R] >>\nendobj\n"
	third := len(pdf)
	pdf += "3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] >>\nendobj\n"
	xref := len(pdf)
	var entries bytes.Buffer
	for _, offset := range []int{0, first, second, third, xref} {
		var entry [7]byte
		if offset > 0 {
			entry[0] = 1
		} else {
			binary.BigEndian.PutUint16(entry[5:], 65535)
		}
		binary.BigEndian.PutUint32(entry[1:5], uint32(offset))
		entries.Write(entry[:])
	}
	pdf += "4 0 obj\n" + invoicePDFStreamFixture("/Type /XRef /Size 5 /W [1 4 2] /Root 1 0 R", entries.String()) + "\nendobj\n"
	pdf += fmt.Sprintf("startxref\n%d\n%%%%EOF\n", xref)
	mime, err := validateInvoiceAttachment([]byte(pdf))
	require.NoError(t, err)
	assert.Equal(t, "application/pdf", mime)
}

func TestInvoicePDFXrefCompressedCatalogActions(t *testing.T) {
	// Synthetic one-page fixtures contain a compressed catalog referenced by a
	// type-2 xref entry. PDF readers load it regardless of its stream /Type.
	for _, name := range []string{"typed", "missing", "mislabeled"} {
		t.Run(name, func(t *testing.T) {
			content, err := os.ReadFile("testdata/invoice-pdf/" + name + ".pdf")
			require.NoError(t, err)
			_, err = validateInvoiceAttachment(content)
			require.Error(t, err, "xref-selected object streams must not hide catalog actions")
		})
	}
}

func TestInvoicePDFObjectStreams(t *testing.T) {
	for _, filtered := range []bool{false, true} {
		for _, active := range []bool{false, true} {
			t.Run(fmt.Sprintf("flate=%t/active=%t", filtered, active), func(t *testing.T) {
				object := `<< /Title (ordinary /AA text) >>`
				if active {
					object = `<< /Open#41ction << /S /JavaScript >> >>`
				}
				content := "10 0 " + object
				filter := ""
				if filtered {
					var compressed bytes.Buffer
					writer := zlib.NewWriter(&compressed)
					_, err := writer.Write([]byte(content))
					require.NoError(t, err)
					require.NoError(t, writer.Close())
					content, filter = compressed.String(), "/Filter [/FlateDecode]"
				}
				pdf := invoicePDFFixture(`<< /Type /Catalog >>`, invoicePDFStreamFixture("/Type /ObjStm /N 1 /First 5 "+filter, content))
				mime, err := validateInvoiceAttachment(pdf)
				if active {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
					assert.Equal(t, "application/pdf", mime)
				}
			})
		}
	}
	for name, dictionary := range map[string]string{
		"unknown filter":        "/Type /ObjStm /N 1 /First 5 /Filter /LZWDecode",
		"unsupported predictor": "/Type /ObjStm /N 1 /First 5 /DecodeParms << /Predictor 12 >>",
		"invalid first":         "/Type /ObjStm /N 1 /First 999999",
		"oversized count":       "/Type /ObjStm /N 100001 /First 5",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := validateInvoiceAttachment(invoicePDFFixture(invoicePDFStreamFixture(dictionary, "10 0 << >>")))
			require.Error(t, err)
		})
	}
}
