package engine

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
)

// PDFBackend is the set of PDF operations Mandarino needs. It is implemented
// with pdfium (the PDF engine of Chrome) in pdfium.go. Page numbers are 1-based.
type PDFBackend interface {
	// Render calls fn for every page, rendered at dpi dots per inch.
	Render(path string, dpi int, fn func(page, total int, img *image.RGBA) error) error
	// Merge concatenates the PDFs in order and returns the new document.
	Merge(inputs []string) ([]byte, error)
	// Split calls fn with a one-page PDF for every page from..to
	// (from = 0 means "from the first page", to = 0 "to the last page").
	Split(path string, from, to int, fn func(page, total int, data []byte) error) error
	// PageCount returns the number of pages.
	PageCount(path string) (int, error)
	// Extract returns a new PDF containing pages from..to (inclusive).
	Extract(path string, from, to int) ([]byte, error)
}

// PDF is the backend in use; nil when no PDF library is available on this platform.
var PDF PDFBackend

var errNoPDF = errors.New("the PDF library (pdfium) is not available: keep it in the same folder as Mandarino")

// pageName returns "doc.png" for a one-page PDF and "doc - page 07.png" otherwise
// (zero-padded so the files sort correctly).
func pageName(in string, page, total int, ext string) string {
	if total == 1 {
		return replaceExt(in, ext)
	}
	return withSuffix(in, fmt.Sprintf(" - page %0*d", len(fmt.Sprint(total)), page), ext)
}

// pdfToImages renders every page of in to a PNG or JPG at 150 dpi.
func pdfToImages(ctx context.Context, in, ext string, prog func(float64)) ([]string, error) {
	if PDF == nil {
		return nil, errNoPDF
	}
	var outs []string
	err := PDF.Render(in, 150, func(page, total int, img *image.RGBA) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		out := UniquePath(pageName(in, page, total, ext))
		if err := saveImage(ctx, img, ext, out, 0); err != nil {
			release(out)
			return err
		}
		outs = append(outs, out)
		if prog != nil {
			prog(float64(page) / float64(total))
		}
		return nil
	})
	return outs, err
}

// mergePDFs joins several PDFs into "<first> (merged).pdf".
func mergePDFs(files []string) (string, error) {
	if PDF == nil {
		return "", errNoPDF
	}
	data, err := PDF.Merge(files)
	if err != nil {
		return "", err
	}
	out := UniquePath(withSuffix(files[0], " (merged)", "pdf"))
	if err := os.WriteFile(out, data, 0o644); err != nil {
		return out, err
	}
	return out, nil
}

// splitPDF writes one PDF per page ("doc - page 03.pdf") for pages from..to.
func splitPDF(ctx context.Context, in string, from, to int, prog func(float64)) ([]string, error) {
	if PDF == nil {
		return nil, errNoPDF
	}
	var outs []string
	err := PDF.Split(in, from, to, func(page, total int, data []byte) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		out := UniquePath(withSuffix(in, fmt.Sprintf(" - page %0*d", len(fmt.Sprint(total)), page), "pdf"))
		if err := os.WriteFile(out, data, 0o644); err != nil {
			release(out)
			return fmt.Errorf("%s: %w", filepath.Base(out), err)
		}
		outs = append(outs, out)
		if prog != nil {
			lo, hi := max(from, 1), to
			if hi < 1 || hi > total {
				hi = total
			}
			prog(float64(page-lo+1) / float64(max(hi-lo+1, 1)))
		}
		return nil
	})
	return outs, err
}

// PageCount returns the number of pages of a PDF (used by the page-range picker).
func PageCount(path string) (int, error) {
	if PDF == nil {
		return 0, errNoPDF
	}
	return PDF.PageCount(path)
}

// extractPDF writes pages from..to into "doc (pages 3-7).pdf" or "doc (page 3).pdf".
func extractPDF(in string, from, to int) (string, error) {
	if PDF == nil {
		return "", errNoPDF
	}
	data, err := PDF.Extract(in, from, to)
	if err != nil {
		return "", err
	}
	suffix := fmt.Sprintf(" (pages %d-%d)", from, to)
	if from == to {
		suffix = fmt.Sprintf(" (page %d)", from)
	}
	out := UniquePath(withSuffix(in, suffix, "pdf"))
	if err := os.WriteFile(out, data, 0o644); err != nil {
		return out, err
	}
	return out, nil
}

// parseRange parses "3-7" into (3, 7).
func parseRange(s string) (int, int, error) {
	var a, b int
	if _, err := fmt.Sscanf(s, "%d-%d", &a, &b); err != nil || a < 1 || b < a {
		return 0, 0, fmt.Errorf("invalid page range: %s", s)
	}
	return a, b, nil
}
