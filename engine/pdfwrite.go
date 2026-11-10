package engine

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"fmt"
	"image"
)

// pdfImage is an image ready to be placed on a PDF page.
type pdfImage struct {
	w, h  int
	jpeg  []byte // if set: original JPEG bytes, embedded as-is (DCTDecode)
	comps int    // colour components of the JPEG (1 = grey, 3 = colour)
	rgb   []byte // otherwise: raw RGB pixels (compressed with Flate/zlib)
}

// fromImage flattens img on white and stores its raw RGB pixels.
func fromImage(img image.Image) pdfImage {
	rgba := flatten(img)
	w, h := rgba.Rect.Dx(), rgba.Rect.Dy()
	rgb := make([]byte, 0, w*h*3)
	for y := 0; y < h; y++ {
		row := rgba.Pix[y*rgba.Stride : y*rgba.Stride+w*4]
		for x := 0; x < w; x++ {
			rgb = append(rgb, row[x*4], row[x*4+1], row[x*4+2])
		}
	}
	return pdfImage{w: w, h: h, rgb: rgb}
}

// writeImagesPDF writes a PDF with one image per page, centred on an A4 page
// (portrait or landscape to match the image) with a small margin.
//
// A PDF is a list of numbered objects followed by a cross-reference table
// ("xref") with the byte offset of each object, so we build everything in a
// buffer while recording offsets, then append the table and the trailer.
func writeImagesPDF(w *bufio.Writer, pages []pdfImage) error {
	var buf bytes.Buffer
	offsets := []int{0} // object 0 does not exist
	obj := func(body []byte) {
		offsets = append(offsets, buf.Len())
		fmt.Fprintf(&buf, "%d 0 obj\n", len(offsets)-1)
		buf.Write(body)
		buf.WriteString("\nendobj\n")
	}
	buf.WriteString("%PDF-1.4\n%\xE2\xE3\xCF\xD3\n")

	n := len(pages)
	// numbering: 1 catalog, 2 page tree, then for every page: page, contents, image
	pageID := func(i int) int { return 3 + i*3 }
	kids := ""
	for i := 0; i < n; i++ {
		kids += fmt.Sprintf("%d 0 R ", pageID(i))
	}
	obj([]byte("<< /Type /Catalog /Pages 2 0 R >>"))
	obj([]byte(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids, n)))

	for i, p := range pages {
		pw, ph := 595.28, 841.89
		if p.w > p.h {
			pw, ph = ph, pw
		}
		const margin = 20.0
		aw, ah := pw-2*margin, ph-2*margin
		s := aw / float64(p.w)
		if v := ah / float64(p.h); v < s {
			s = v
		}
		dw, dh := float64(p.w)*s, float64(p.h)*s
		x, y := (pw-dw)/2, (ph-dh)/2

		obj([]byte(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.2f %.2f] /Resources << /XObject << /Im0 %d 0 R >> /ProcSet [/PDF /ImageC /ImageB] >> /Contents %d 0 R >>",
			pw, ph, pageID(i)+2, pageID(i)+1)))
		content := fmt.Sprintf("q %.4f 0 0 %.4f %.4f %.4f cm /Im0 Do Q", dw, dh, x, y)
		obj([]byte(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content)))

		var data []byte
		var dict string
		if p.jpeg != nil {
			cs := "/DeviceRGB"
			if p.comps == 1 {
				cs = "/DeviceGray"
			}
			data = p.jpeg
			dict = fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace %s /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>", p.w, p.h, cs, len(data))
		} else {
			var z bytes.Buffer
			zw, _ := zlib.NewWriterLevel(&z, zlib.DefaultCompression)
			zw.Write(p.rgb)
			zw.Close()
			data = z.Bytes()
			dict = fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /FlateDecode /Length %d >>", p.w, p.h, len(data))
		}
		body := append([]byte(dict+"\nstream\n"), data...)
		body = append(body, []byte("\nendstream")...)
		obj(body)
	}

	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, o := range offsets[1:] {
		fmt.Fprintf(&buf, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	_, err := w.Write(buf.Bytes())
	return err
}
