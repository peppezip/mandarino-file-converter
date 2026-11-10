package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestActions(t *testing.T) {
	a, err := ActionsFor([]string{`C:\x\foto.PNG`})
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range a {
		if x.ID == "to:png" {
			t.Fatal("must not offer the same format")
		}
	}
	if _, err := ActionsFor([]string{"a.png", "b.mp4"}); err != ErrMixed {
		t.Fatal("expected a mixed selection error")
	}
	a, _ = ActionsFor([]string{"a.pdf", "b.pdf"})
	found := false
	for _, x := range a {
		found = found || x.ID == "pdf:merge"
	}
	if !found {
		t.Fatal("Merge is missing")
	}
	if NormExt("x.JPEG") != "jpg" || KindOf("film.MKV") != KindVideo {
		t.Fatal("extensions")
	}
}

func testImage() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 120, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 120; x++ {
			a := uint8(255)
			if x < 20 {
				a = 0
			}
			img.SetNRGBA(x, y, color.NRGBA{uint8(x * 2), uint8(y * 3), 150, a})
		}
	}
	return img
}

// jpegWithOrientation builds a 40x20 JPEG carrying an EXIF Orientation tag.
func jpegWithOrientation(t *testing.T, o uint16) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 40; x++ {
			img.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	var j bytes.Buffer
	jpeg.Encode(&j, img, nil)
	b := j.Bytes()
	tiff := []byte("II*\x00\x08\x00\x00\x00\x01\x00")
	entry := make([]byte, 12)
	binary.LittleEndian.PutUint16(entry, 0x0112)
	binary.LittleEndian.PutUint16(entry[2:], 3)
	binary.LittleEndian.PutUint32(entry[4:], 1)
	binary.LittleEndian.PutUint16(entry[8:], o)
	tiff = append(tiff, entry...)
	tiff = append(tiff, 0, 0, 0, 0)
	payload := append([]byte("Exif\x00\x00"), tiff...)
	seg := []byte{0xFF, 0xE1, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}
	seg = append(seg, payload...)
	out := append([]byte{}, b[:2]...)
	out = append(out, seg...)
	return append(out, b[2:]...)
}

// ffmpegBin returns the ffmpeg used by the engine (bundled copy first), so the
// tests behave the same everywhere, even where the system ffmpeg is old.
func ffmpegBin(t *testing.T) string {
	bin, err := FFmpegPath()
	if err != nil {
		t.Skip("ffmpeg not available")
	}
	return bin
}

func setup(t *testing.T) (string, context.Context) {
	if _, err := FFmpegPath(); err != nil {
		t.Skip("ffmpeg not available")
	}
	dir := t.TempDir()
	return dir, context.Background()
}

func TestImages(t *testing.T) {
	dir, ctx := setup(t)
	src := filepath.Join(dir, "prova.png")
	f, _ := os.Create(src)
	png.Encode(f, testImage())
	f.Close()

	for _, ext := range []string{"jpg", "webp", "bmp", "tiff", "gif", "ico"} {
		r := Run(ctx, "to:"+ext, []string{src}, nil)
		if len(r.Failed) > 0 {
			t.Fatalf("%s: %v", ext, r.Failed[0].Err)
		}
		out := r.Outputs[0]
		if !strings.HasSuffix(out, "."+ext) {
			t.Fatalf("nome errato %s", out)
		}
		// read it back to make sure the file is valid
		img, err := loadImage(ctx, out)
		if err != nil {
			t.Fatalf("%s unreadable: %v", ext, err)
		}
		b := img.Bounds()
		want := image.Pt(120, 80)
		if ext == "ico" {
			want = image.Pt(256, 256)
		}
		if b.Size() != want {
			t.Fatalf("%s: size %v", ext, b.Size())
		}
		t.Logf("%s ok (%v)", filepath.Base(out), b.Size())
	}
	// second conversion: unique name
	r := Run(ctx, "to:jpg", []string{src}, nil)
	if filepath.Base(r.Outputs[0]) != "prova (1).jpg" {
		t.Fatalf("wrong unique name: %s", r.Outputs[0])
	}
	// webp -> png (decoded by ffmpeg)
	r = Run(ctx, "to:png", []string{filepath.Join(dir, "prova.webp")}, nil)
	if len(r.Failed) > 0 {
		t.Fatal(r.Failed[0].Err)
	}
	// halve and compress
	r = Run(ctx, "img:half", []string{src}, nil)
	if len(r.Failed) > 0 {
		t.Fatal(r.Failed[0].Err)
	}
	img, _ := loadImage(ctx, r.Outputs[0])
	if img.Bounds().Size() != image.Pt(60, 40) {
		t.Fatalf("50%%: %v", img.Bounds().Size())
	}
	r = Run(ctx, "img:compress", []string{src}, nil)
	if len(r.Failed) > 0 {
		t.Fatal(r.Failed[0].Err)
	}
	t.Logf("compressed: %s", filepath.Base(r.Outputs[0]))
	r = Run(ctx, "img:compress", []string{filepath.Join(dir, "prova.bmp")}, nil)
	if len(r.Failed) > 0 || !strings.HasSuffix(r.Outputs[0], "prova (compressed).jpg") {
		t.Fatalf("compress bmp: %+v", r)
	}

	// EXIF orientation
	jp := filepath.Join(dir, "ruotata.jpg")
	os.WriteFile(jp, jpegWithOrientation(t, 6), 0o644)
	img, err := loadImage(ctx, jp)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Size() != image.Pt(20, 40) {
		t.Fatalf("EXIF rotation not applied: %v", img.Bounds().Size())
	}

	// images -> PDF
	r = Run(ctx, "img:pdf", []string{src, jp, filepath.Join(dir, "prova.jpg")}, nil)
	if len(r.Failed) > 0 {
		t.Fatal(r.Failed[0].Err)
	}
	t.Logf("PDF: %s", r.Outputs[0])
}

func TestMedia(t *testing.T) {
	dir, ctx := setup(t)
	vid := filepath.Join(dir, "clip.mov")
	cmd := exec.Command(ffmpegBin(t), "-v", "error", "-f", "lavfi", "-i", "testsrc=size=320x240:rate=25:duration=2",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2", "-c:v", "mpeg4", "-c:a", "pcm_s16le", "-shortest", vid)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	acts := []string{"to:mp4", "to:mkv", "to:webm", "to:avi", "vid:gif", "vid:mp3", "vid:compress", "vid:mute"}
	for _, a := range acts {
		var last float64
		r := Run(ctx, a, []string{vid}, func(i, n int, f float64) { last = f })
		if len(r.Failed) > 0 {
			t.Fatalf("%s: %v", a, r.Failed[0].Err)
		}
		st, _ := os.Stat(r.Outputs[0])
		t.Logf("%s -> %s (%d bytes, progress %.2f)", a, filepath.Base(r.Outputs[0]), st.Size(), last)
	}
	mp3 := filepath.Join(dir, "clip.mp3")
	for _, a := range []string{"to:wav", "to:flac", "to:m4a", "to:ogg", "to:opus"} {
		r := Run(ctx, a, []string{mp3}, nil)
		if len(r.Failed) > 0 {
			t.Fatalf("%s: %v", a, r.Failed[0].Err)
		}
		t.Logf("%s -> %s", a, filepath.Base(r.Outputs[0]))
	}
	// error: invalid file, no partial output left behind
	bad := filepath.Join(dir, "rotto.mp4")
	os.WriteFile(bad, []byte("this is not a video"), 0o644)
	r := Run(ctx, "to:mkv", []string{bad}, nil)
	if len(r.Failed) != 1 {
		t.Fatal("expected an error")
	}
	t.Logf("error shown: %v", r.Failed[0].Err)
	if _, err := os.Stat(filepath.Join(dir, "rotto.mkv")); err == nil {
		t.Fatal("partial file not deleted")
	}
}

// Simulated screen recording: variable frame rate and a 10 MHz timescale (like Windows).
func TestCompressScreenRecording(t *testing.T) {
	dir, ctx := setup(t)
	rec := filepath.Join(dir, "Registrazione 2026-09-24 165304.mp4")
	cmd := exec.Command(ffmpegBin(t), "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=2560x1441:rate=60:duration=3",
		"-f", "lavfi", "-i", "sine=duration=3", "-f", "lavfi", "-i", "sine=frequency=880:duration=3",
		"-map", "0:v", "-map", "1:a", "-map", "2:a",
		"-vf", "select='not(mod(n\\,3))+not(mod(n\\,7))',setpts=N/(37*TB)+random(0)*0.004/TB,crop=2559:1441",
		"-fps_mode", "vfr", "-c:v", "libx264", "-pix_fmt", "yuvj420p", "-c:a", "aac", "-video_track_timescale", "10000000", "-shortest", rec)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	r := Run(ctx, "vid:compress", []string{rec}, nil)
	if len(r.Failed) > 0 {
		t.Fatal(r.Failed[0].Err)
	}
	// read the result back with "ffmpeg -i" (ffprobe is not bundled)
	out, _ := exec.Command(ffmpegBin(t), "-hide_banner", "-i", r.Outputs[0]).CombinedOutput()
	t.Logf("%s\n%s", filepath.Base(r.Outputs[0]), out)
	s := string(out)
	if !strings.Contains(s, "1920x1080") || !strings.Contains(s, "yuv420p") || strings.Count(s, "Audio:") != 1 {
		t.Fatal("unexpected output")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Fatalf("temporary files left behind: %v", entries)
	}
}

type fakePDF struct{ n int }

func (f fakePDF) Render(string, int, func(int, int, *image.RGBA) error) error { return nil }
func (f fakePDF) Merge([]string) ([]byte, error)                              { return []byte("%PDF"), nil }
func (f fakePDF) PageCount(string) (int, error)                               { return f.n, nil }
func (f fakePDF) Extract(_ string, a, b int) ([]byte, error) {
	return []byte(fmt.Sprintf("%d-%d", a, b)), nil
}
func (f fakePDF) Split(_ string, a, b int, fn func(int, int, []byte) error) error {
	if a < 1 {
		a = 1
	}
	if b < 1 {
		b = f.n
	}
	for i := a; i <= b; i++ {
		if err := fn(i, f.n, []byte("x")); err != nil {
			return err
		}
	}
	return nil
}

func TestPDFRange(t *testing.T) {
	old := PDF
	PDF = fakePDF{n: 12}
	defer func() { PDF = old }()
	dir := t.TempDir()
	in := filepath.Join(dir, "doc.pdf")
	os.WriteFile(in, []byte("%PDF"), 0o644)
	var last float64
	r := Run(context.Background(), "pdf:extract:3-7", []string{in}, nil)
	if len(r.Failed) > 0 || filepath.Base(r.Outputs[0]) != "doc (pages 3-7).pdf" {
		t.Fatalf("%+v", r)
	}
	r = Run(context.Background(), "pdf:extract:5-5", []string{in}, nil)
	if filepath.Base(r.Outputs[0]) != "doc (page 5).pdf" {
		t.Fatalf("%+v", r)
	}
	r = Run(context.Background(), "pdf:split:10-12", []string{in}, func(i, n int, f float64) { last = f })
	if len(r.Outputs) != 3 || filepath.Base(r.Outputs[0]) != "doc - page 10.pdf" || last != 1 {
		t.Fatalf("%+v %v", r, last)
	}
	r = Run(context.Background(), "pdf:extract:7-3", []string{in}, nil)
	if len(r.Failed) != 1 {
		t.Fatal("reversed range accepted")
	}
}

// TestMain lets the tests find the bundled tools: set MANDARINO_TOOLS to a folder
// containing ffmpeg and the pdfium library (libpdfium.so / pdfium.dll).
func TestMain(m *testing.M) {
	ToolDir = os.Getenv("MANDARINO_TOOLS")
	os.Exit(m.Run())
}

// TestPDFium exercises the real pdfium library (skipped when it is not available).
func TestPDFium(t *testing.T) {
	if _, err := PageCount("does-not-exist.pdf"); err == errNoPDF {
		t.Skip("pdfium not available (set MANDARINO_TOOLS)")
	}
	dir := t.TempDir()
	ctx := context.Background()

	// a 3-page PDF made with our own writer
	src := filepath.Join(dir, "doc.pdf")
	var pages []pdfImage
	for _, c := range []color.RGBA{{255, 0, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 255}} {
		img := image.NewRGBA(image.Rect(0, 0, 60, 80))
		for i := 0; i < len(img.Pix); i += 4 {
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, 255
		}
		pages = append(pages, fromImage(img))
	}
	if err := writeFile(src, func(w *bufio.Writer) error { return writeImagesPDF(w, pages) }); err != nil {
		t.Fatal(err)
	}
	if n, err := PageCount(src); err != nil || n != 3 {
		t.Fatalf("PageCount = %d, %v", n, err)
	}

	// pages -> PNG: page 2 must be green in the middle
	r := Run(ctx, "pdf:png", []string{src}, nil)
	if len(r.Failed) > 0 || len(r.Outputs) != 3 {
		t.Fatalf("render: %+v", r)
	}
	img, err := loadImage(ctx, r.Outputs[1])
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	if cr, cg, cb, _ := img.At(b.Dx()/2, b.Dy()/2).RGBA(); cg>>8 < 200 || cr>>8 > 60 || cb>>8 > 60 {
		t.Fatalf("page 2 is not green: %v", img.At(b.Dx()/2, b.Dy()/2))
	}

	// extract, split, merge
	r = Run(ctx, "pdf:extract:2-3", []string{src}, nil)
	if len(r.Failed) > 0 || filepath.Base(r.Outputs[0]) != "doc (pages 2-3).pdf" {
		t.Fatalf("extract: %+v", r)
	}
	if n, _ := PageCount(r.Outputs[0]); n != 2 {
		t.Fatalf("extracted PDF has %d pages", n)
	}
	r = Run(ctx, "pdf:split", []string{src}, nil)
	if len(r.Outputs) != 3 {
		t.Fatalf("split: %+v", r)
	}
	r = Run(ctx, "pdf:merge", []string{src, src}, nil)
	if len(r.Failed) > 0 {
		t.Fatal(r.Failed[0].Err)
	}
	if n, _ := PageCount(r.Outputs[0]); n != 6 {
		t.Fatalf("merged PDF has %d pages", n)
	}
	// a broken file gives a clear error
	bad := filepath.Join(dir, "bad.pdf")
	os.WriteFile(bad, []byte("not a pdf"), 0o644)
	if _, err := PageCount(bad); err == nil {
		t.Fatal("expected an error for a broken PDF")
	} else {
		t.Logf("broken PDF: %v", err)
	}
}
