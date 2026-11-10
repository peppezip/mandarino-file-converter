package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

// ---------- reading ----------

// loadImage opens an image. JPG, PNG and GIF are decoded by Go's standard library
// (JPGs are also rotated according to their EXIF orientation, like phones expect).
// Every other format (BMP, TIFF, WebP, HEIC, AVIF, ICO...) is first converted to a
// temporary PNG by ffmpeg.
func loadImage(ctx context.Context, path string) (image.Image, error) {
	switch NormExt(path) {
	case "jpg", "png", "gif":
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		img, err := decodeStd(data)
		if err == nil {
			if NormExt(path) == "jpg" {
				img = applyOrientation(img, jpegOrientation(data))
			}
			return img, nil
		}
		// wrong extension or unusual variant: let ffmpeg try
	}
	tmp, err := tempFile(".png")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp)
	if err := runFFmpeg(ctx, []string{"-i", path, "-frames:v", "1", "-pix_fmt", "rgba", tmp}, nil); err != nil {
		return nil, err
	}
	f, err := os.Open(tmp)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(bufio.NewReader(f))
}

// decodeStd decodes JPG/PNG/GIF. For GIFs it returns the first frame drawn on the
// full logical canvas (a GIF frame can be smaller than the image).
func decodeStd(data []byte) (image.Image, error) {
	if bytes.HasPrefix(data, []byte("GIF8")) {
		g, err := gif.DecodeAll(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		if len(g.Image) == 0 {
			return nil, fmt.Errorf("empty GIF")
		}
		canvas := image.NewNRGBA(image.Rect(0, 0, g.Config.Width, g.Config.Height))
		draw.Draw(canvas, g.Image[0].Bounds(), g.Image[0], g.Image[0].Bounds().Min, draw.Src)
		return canvas, nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

// tempFile creates an empty temporary file with the given extension and returns its path.
func tempFile(ext string) (string, error) {
	f, err := os.CreateTemp("", "mandarino-*"+ext)
	if err != nil {
		return "", err
	}
	name := f.Name()
	f.Close()
	return name, nil
}

// jpegOrientation reads the EXIF Orientation tag (1..8) of a JPEG; 1 when absent.
// It walks the JPEG markers until it finds the APP1 "Exif" segment.
func jpegOrientation(b []byte) int {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return 1
	}
	i := 2
	for i+4 <= len(b) {
		if b[i] != 0xFF {
			return 1
		}
		marker := b[i+1]
		if marker == 0xD9 || marker == 0xDA { // end of image / start of scan: no EXIF
			return 1
		}
		size := int(b[i+2])<<8 | int(b[i+3])
		if size < 2 || i+2+size > len(b) {
			return 1
		}
		seg := b[i+4 : i+2+size]
		if marker == 0xE1 && len(seg) > 14 && string(seg[:6]) == "Exif\x00\x00" {
			return exifOrientation(seg[6:])
		}
		i += 2 + size
	}
	return 1
}

// exifOrientation looks for tag 0x0112 in the first IFD of a TIFF/EXIF block.
func exifOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	off := int(bo.Uint32(t[4:8]))
	if off+2 > len(t) {
		return 1
	}
	n := int(bo.Uint16(t[off:]))
	for k := 0; k < n; k++ {
		e := off + 2 + k*12
		if e+12 > len(t) {
			return 1
		}
		if bo.Uint16(t[e:]) == 0x0112 {
			v := int(bo.Uint16(t[e+8:]))
			if v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}

// applyOrientation rotates/mirrors the image according to the EXIF orientation,
// so a portrait photo taken with a phone stays portrait.
func applyOrientation(src image.Image, o int) image.Image {
	if o <= 1 || o > 8 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	swap := o >= 5
	dw, dh := w, h
	if swap {
		dw, dh = h, w
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			dst.Set(dx, dy, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

// ---------- helpers ----------

// toNRGBA converts any image to non-premultiplied RGBA with origin (0,0).
func toNRGBA(src image.Image) *image.NRGBA {
	if n, ok := src.(*image.NRGBA); ok && n.Rect.Min == (image.Point{}) {
		return n
	}
	b := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	return dst
}

// flatten draws the image on a white background (for formats without transparency).
func flatten(src image.Image) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Over)
	return dst
}

// resize scales the image with a Catmull-Rom filter (good quality both when
// shrinking and when enlarging). It runs two 1-D passes: horizontal, then vertical.
func resize(src image.Image, w, h int) *image.NRGBA {
	s := toNRGBA(src)
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if s.Rect.Dx() == w && s.Rect.Dy() == h {
		return s
	}
	tmp := resample1D(s, w, true)
	return resample1D(tmp, h, false)
}

// catmullRom is the cubic filter kernel used by resize.
func catmullRom(x float64) float64 {
	x = math.Abs(x)
	if x < 1 {
		return (1.5*x-2.5)*x*x + 1
	}
	if x < 2 {
		return ((-0.5*x+2.5)*x-4)*x + 2
	}
	return 0
}

// resample1D resizes along one axis (horiz=true: width, false: height).
// Colours are weighted by alpha so transparent pixels do not bleed dark fringes.
func resample1D(s *image.NRGBA, n int, horiz bool) *image.NRGBA {
	sw, sh := s.Rect.Dx(), s.Rect.Dy()
	srcLen := sw
	if !horiz {
		srcLen = sh
	}
	var dst *image.NRGBA
	if horiz {
		dst = image.NewNRGBA(image.Rect(0, 0, n, sh))
	} else {
		dst = image.NewNRGBA(image.Rect(0, 0, sw, n))
	}
	scale := float64(srcLen) / float64(n)
	support := 2.0
	fscale := 1.0
	if scale > 1 {
		support *= scale
		fscale = scale
	}
	other := sh
	if !horiz {
		other = sw
	}
	type tap struct {
		idx []int
		w   []float64
	}
	taps := make([]tap, n)
	for i := 0; i < n; i++ {
		center := (float64(i)+0.5)*scale - 0.5
		lo := int(math.Floor(center - support))
		hi := int(math.Ceil(center + support))
		var t tap
		sum := 0.0
		for j := lo; j <= hi; j++ {
			wgt := catmullRom((float64(j) - center) / fscale)
			if wgt == 0 {
				continue
			}
			jj := j
			if jj < 0 {
				jj = 0
			} else if jj >= srcLen {
				jj = srcLen - 1
			}
			t.idx = append(t.idx, jj)
			t.w = append(t.w, wgt)
			sum += wgt
		}
		if sum != 0 {
			for k := range t.w {
				t.w[k] /= sum
			}
		}
		taps[i] = t
	}
	for o := 0; o < other; o++ {
		for i := 0; i < n; i++ {
			var r, g, b, a float64
			for k, j := range taps[i].idx {
				var p int
				if horiz {
					p = s.PixOffset(j, o)
				} else {
					p = s.PixOffset(o, j)
				}
				pa := float64(s.Pix[p+3])
				wa := taps[i].w[k] * pa
				r += float64(s.Pix[p]) * wa
				g += float64(s.Pix[p+1]) * wa
				b += float64(s.Pix[p+2]) * wa
				a += wa
			}
			var q int
			if horiz {
				q = dst.PixOffset(i, o)
			} else {
				q = dst.PixOffset(o, i)
			}
			if a > 0.5 {
				dst.Pix[q] = clamp8(r / a)
				dst.Pix[q+1] = clamp8(g / a)
				dst.Pix[q+2] = clamp8(b / a)
			}
			dst.Pix[q+3] = clamp8(a)
		}
	}
	return dst
}

// clamp8 rounds v to the 0..255 range.
func clamp8(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}

// ---------- writing ----------

// saveImage writes img to out in format ext. quality: 0 = default, otherwise 1..100.
// JPG, PNG, BMP, ICO and PDF are written in Go; GIF, TIFF and WebP go through ffmpeg.
func saveImage(ctx context.Context, img image.Image, ext, out string, quality int) error {
	switch ext {
	case "jpg":
		q := 92
		if quality > 0 {
			q = quality
		}
		return writeFile(out, func(w *bufio.Writer) error {
			return jpeg.Encode(w, flatten(img), &jpeg.Options{Quality: q})
		})
	case "png":
		enc := png.Encoder{CompressionLevel: png.DefaultCompression}
		return writeFile(out, func(w *bufio.Writer) error { return enc.Encode(w, img) })
	case "bmp":
		return writeFile(out, func(w *bufio.Writer) error { return encodeBMP(w, flatten(img)) })
	case "ico":
		return writeFile(out, func(w *bufio.Writer) error { return encodeICO(w, img) })
	case "gif":
		return viaFFmpeg(ctx, img, out, []string{"-vf", "split[a][b];[a]palettegen=reserve_transparent=1[p];[b][p]paletteuse=alpha_threshold=128"})
	case "tiff":
		return viaFFmpeg(ctx, img, out, []string{"-c:v", "tiff", "-compression_algo", "deflate"})
	case "webp":
		q := 90
		if quality > 0 {
			q = quality
		}
		return viaFFmpeg(ctx, img, out, []string{"-c:v", "libwebp", "-quality", fmt.Sprint(q)})
	case "pdf":
		return writeFile(out, func(w *bufio.Writer) error { return writeImagesPDF(w, []pdfImage{fromImage(img)}) })
	}
	return fmt.Errorf("unsupported image format: %s", ext)
}

// viaFFmpeg saves a temporary PNG and lets ffmpeg convert it.
func viaFFmpeg(ctx context.Context, img image.Image, out string, args []string) error {
	tmp, err := tempFile(".png")
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := writeFile(tmp, func(w *bufio.Writer) error { return enc.Encode(w, img) }); err != nil {
		return err
	}
	return runFFmpeg(ctx, cat([]string{"-i", tmp}, args, []string{"-frames:v", "1", out}), nil)
}

// writeFile creates path, lets fn write it through a buffer, and deletes the
// file again if anything fails (no half-written outputs).
func writeFile(path string, fn func(w *bufio.Writer) error) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	err = fn(w)
	if err == nil {
		err = w.Flush()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
	}
	return err
}

// encodeBMP writes a 24-bit uncompressed BMP (rows are stored bottom-up).
func encodeBMP(w *bufio.Writer, img *image.RGBA) error {
	width, height := img.Rect.Dx(), img.Rect.Dy()
	rowSize := (width*3 + 3) &^ 3
	dataSize := rowSize * height
	hdr := make([]byte, 54)
	hdr[0], hdr[1] = 'B', 'M'
	binary.LittleEndian.PutUint32(hdr[2:], uint32(54+dataSize))
	binary.LittleEndian.PutUint32(hdr[10:], 54)
	binary.LittleEndian.PutUint32(hdr[14:], 40)
	binary.LittleEndian.PutUint32(hdr[18:], uint32(width))
	binary.LittleEndian.PutUint32(hdr[22:], uint32(height))
	binary.LittleEndian.PutUint16(hdr[26:], 1)
	binary.LittleEndian.PutUint16(hdr[28:], 24)
	binary.LittleEndian.PutUint32(hdr[34:], uint32(dataSize))
	binary.LittleEndian.PutUint32(hdr[38:], 2835) // 72 dpi
	binary.LittleEndian.PutUint32(hdr[42:], 2835)
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	row := make([]byte, rowSize)
	for y := height - 1; y >= 0; y-- {
		for x := 0; x < width; x++ {
			p := img.PixOffset(x, y)
			row[x*3] = img.Pix[p+2]
			row[x*3+1] = img.Pix[p+1]
			row[x*3+2] = img.Pix[p]
		}
		if _, err := w.Write(row); err != nil {
			return err
		}
	}
	return nil
}

// encodeICO writes a Windows icon with 256, 64, 48, 32 and 16 px images.
// Each size is stored as an embedded PNG (supported since Windows Vista).
func encodeICO(w *bufio.Writer, img image.Image) error {
	b := img.Bounds()
	side := b.Dx()
	if b.Dy() > side {
		side = b.Dy()
	}
	// make the image square by adding transparent borders
	sq := image.NewNRGBA(image.Rect(0, 0, side, side))
	off := image.Pt((side-b.Dx())/2, (side-b.Dy())/2)
	draw.Draw(sq, image.Rectangle{off, off.Add(b.Size())}, img, b.Min, draw.Src)

	sizes := []int{256, 64, 48, 32, 16}
	var blobs [][]byte
	for _, s := range sizes {
		var buf bytes.Buffer
		if err := png.Encode(&buf, resize(sq, s, s)); err != nil {
			return err
		}
		blobs = append(blobs, buf.Bytes())
	}
	head := make([]byte, 6+16*len(sizes))
	binary.LittleEndian.PutUint16(head[2:], 1) // type 1 = icon
	binary.LittleEndian.PutUint16(head[4:], uint16(len(sizes)))
	offset := len(head)
	for i, s := range sizes {
		e := head[6+16*i:]
		if s < 256 {
			e[0], e[1] = byte(s), byte(s)
		}
		binary.LittleEndian.PutUint16(e[4:], 1)
		binary.LittleEndian.PutUint16(e[6:], 32)
		binary.LittleEndian.PutUint32(e[8:], uint32(len(blobs[i])))
		binary.LittleEndian.PutUint32(e[12:], uint32(offset))
		offset += len(blobs[i])
	}
	if _, err := w.Write(head); err != nil {
		return err
	}
	for _, bl := range blobs {
		if _, err := w.Write(bl); err != nil {
			return err
		}
	}
	return nil
}

// ---------- image actions ----------

// encodableExt reports whether we can also write format e.
func encodableExt(e string) bool {
	switch e {
	case "jpg", "png", "webp", "bmp", "tiff", "gif", "ico":
		return true
	}
	return false
}

// convertImage converts one image to format ext, next to the original.
func convertImage(ctx context.Context, in, ext string) (string, error) {
	img, err := loadImage(ctx, in)
	if err != nil {
		return "", err
	}
	out := UniquePath(replaceExt(in, ext))
	return out, saveImage(ctx, img, ext, out, 0)
}

// halveImage writes a copy at half the width and height ("photo (50%).png").
func halveImage(ctx context.Context, in string) (string, error) {
	img, err := loadImage(ctx, in)
	if err != nil {
		return "", err
	}
	b := img.Bounds()
	small := resize(img, (b.Dx()+1)/2, (b.Dy()+1)/2)
	ext := NormExt(in)
	if !encodableExt(ext) {
		ext = "jpg"
	}
	out := UniquePath(withSuffix(in, " (50%)", ext))
	return out, saveImage(ctx, small, ext, out, 0)
}

// compressImage makes an image smaller on disk:
//   - PNG:  quantized to 256 colours, stays PNG (transparency kept)
//   - WebP: re-encoded at quality 70
//   - else: saved as JPG quality 75, long side limited to 2560 px
func compressImage(ctx context.Context, in string) (string, error) {
	ext := NormExt(in)
	switch ext {
	case "png":
		// reduce to 256 colours (like pngquant): usually 60-80% smaller
		out := UniquePath(withSuffix(in, " (compressed)", "png"))
		err := runFFmpeg(ctx, []string{"-i", in, "-vf", "split[a][b];[a]palettegen=max_colors=256:reserve_transparent=1:stats_mode=full[p];[b][p]paletteuse=dither=sierra2_4a:alpha_threshold=128", "-frames:v", "1", "-pix_fmt", "pal8", out}, nil)
		return out, err
	case "webp":
		img, err := loadImage(ctx, in)
		if err != nil {
			return "", err
		}
		out := UniquePath(withSuffix(in, " (compressed)", "webp"))
		return out, saveImage(ctx, img, "webp", out, 70)
	default:
		img, err := loadImage(ctx, in)
		if err != nil {
			return "", err
		}
		// limit the long side to 2560 px and save as JPG, quality 75
		b := img.Bounds()
		if m := max(b.Dx(), b.Dy()); m > 2560 {
			f := 2560.0 / float64(m)
			img = resize(img, int(float64(b.Dx())*f+0.5), int(float64(b.Dy())*f+0.5))
		}
		out := UniquePath(withSuffix(in, " (compressed)", "jpg"))
		return out, saveImage(ctx, img, "jpg", out, 75)
	}
}

// imagesToPDF puts the images in one PDF, one image per page.
func imagesToPDF(ctx context.Context, files []string, prog func(i int)) (string, error) {
	pages := make([]pdfImage, 0, len(files))
	for i, f := range files {
		if prog != nil {
			prog(i)
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		data, err := os.ReadFile(f)
		if err != nil {
			return "", err
		}
		// JPGs that need no rotation are embedded as they are (no quality loss)
		if NormExt(f) == "jpg" && jpegOrientation(data) == 1 {
			if cfg, err := jpeg.DecodeConfig(bytes.NewReader(data)); err == nil && (cfg.ColorModel == color.YCbCrModel || cfg.ColorModel == color.GrayModel) {
				comps := 3
				if cfg.ColorModel == color.GrayModel {
					comps = 1
				}
				pages = append(pages, pdfImage{w: cfg.Width, h: cfg.Height, jpeg: data, comps: comps})
				continue
			}
		}
		img, err := loadImage(ctx, f)
		if err != nil {
			return "", fmt.Errorf("%s: %w", filepath.Base(f), err)
		}
		pages = append(pages, fromImage(img))
	}
	var out string
	if len(files) == 1 {
		out = UniquePath(replaceExt(files[0], "pdf"))
	} else {
		out = UniquePath(withSuffix(files[0], " (combined)", "pdf"))
	}
	return out, writeFile(out, func(w *bufio.Writer) error { return writeImagesPDF(w, pages) })
}
