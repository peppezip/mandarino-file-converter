// Package engine contains everything Mandarino needs to convert files.
// It has no user interface, so it can be tested on any platform with
// "go test ./engine" (the app itself is Windows-only; the engine also builds
// on Linux and macOS so that tests and CI can run there).
//
// The work is split this way:
//
//   - catalog.go   decides which actions make sense for a selection of files
//   - run.go       executes an action on a list of files
//   - ffmpeg.go    builds and runs ffmpeg commands (audio, video, some images)
//   - images.go    reads, resizes and writes images (pure Go where possible)
//   - pdfwrite.go  a tiny PDF writer used to turn images into a PDF
//   - pdf.go       PDF features backed by the pdfium library
//   - pdfium*.go   the pdfium bindings (Windows: syscall, Linux: cgo/dlopen)
//   - names.go     safe output file names ("photo (1).jpg", ...)
package engine

import (
	"errors"
	"path/filepath"
	"strings"
)

// Kind is the family a file belongs to. A selection can only contain
// files of one kind, because each kind offers different actions.
type Kind int

const (
	KindNone Kind = iota
	KindImage
	KindVideo
	KindAudio
	KindPDF
)

// extKind maps a normalized extension (see NormExt) to its family.
var extKind = map[string]Kind{
	// images (HEIC, AVIF and TGA can be read but are never offered as targets)
	"jpg": KindImage, "png": KindImage, "webp": KindImage, "bmp": KindImage,
	"tiff": KindImage, "gif": KindImage, "ico": KindImage, "heic": KindImage,
	"heif": KindImage, "avif": KindImage, "tga": KindImage,
	// video
	"mp4": KindVideo, "mov": KindVideo, "mkv": KindVideo, "webm": KindVideo,
	"avi": KindVideo, "wmv": KindVideo, "flv": KindVideo, "mpg": KindVideo,
	"3gp": KindVideo, "ts": KindVideo, "mts": KindVideo, "m2ts": KindVideo,
	// audio
	"mp3": KindAudio, "wav": KindAudio, "flac": KindAudio, "m4a": KindAudio,
	"aac": KindAudio, "ogg": KindAudio, "opus": KindAudio, "wma": KindAudio,
	"aiff": KindAudio, "amr": KindAudio,
	// documents
	"pdf": KindPDF,
}

// NormExt returns the lower-case extension of path without the dot,
// with common synonyms merged (".jpeg" -> "jpg", ".tif" -> "tiff", ...).
func NormExt(path string) string {
	e := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	switch e {
	case "jpeg", "jfif", "jpe":
		return "jpg"
	case "tif":
		return "tiff"
	case "aif":
		return "aiff"
	case "m4v":
		return "mp4"
	case "mpeg":
		return "mpg"
	case "oga":
		return "ogg"
	}
	return e
}

// RawExt returns the lower-case extension of path exactly as written
// (no synonym merging). Used when an output must keep the input's format.
func RawExt(path string) string {
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
}

// KindOf returns the family of a file, based on its extension.
func KindOf(path string) Kind { return extKind[NormExt(path)] }

// Action is one entry of the wheel.
type Action struct {
	ID    string // internal identifier understood by Run, e.g. "to:png"
	Label string // short text shown inside a tangerine segment, e.g. "PNG"
	Desc  string // longer description used in notifications, e.g. "Convert to PNG"
}

// conv builds a plain "convert to <format>" action.
func conv(ext, label string) Action {
	return Action{ID: "to:" + ext, Label: label, Desc: "Convert to " + label}
}

// Target formats offered for each family.
var (
	imageTargets = []Action{
		conv("jpg", "JPG"), conv("png", "PNG"), conv("webp", "WebP"),
		conv("bmp", "BMP"), conv("tiff", "TIFF"), conv("gif", "GIF"),
		conv("ico", "ICO"),
	}
	videoTargets = []Action{
		conv("mp4", "MP4"), conv("mkv", "MKV"), conv("mov", "MOV"),
		conv("webm", "WebM"), conv("avi", "AVI"),
	}
	audioTargets = []Action{
		conv("mp3", "MP3"), conv("wav", "WAV"), conv("flac", "FLAC"),
		conv("m4a", "M4A"), conv("ogg", "OGG"), conv("opus", "Opus"),
	}
)

// Errors shown to the user (kept short: they end up in a notification).
var (
	ErrNoFiles     = errors.New("No files selected")
	ErrMixed       = errors.New("Select files of the same type (only images, only videos, only audio or only PDFs)")
	ErrUnsupported = errors.New("This file type is not supported")
)

// SelectionKind checks that all files belong to the same family and returns it.
func SelectionKind(files []string) (Kind, error) {
	if len(files) == 0 {
		return KindNone, ErrNoFiles
	}
	k := KindNone
	for _, f := range files {
		fk := KindOf(f)
		if fk == KindNone {
			return KindNone, ErrUnsupported
		}
		if k != KindNone && fk != k {
			return KindNone, ErrMixed
		}
		k = fk
	}
	return k, nil
}

// commonExt returns the extension shared by all files, or "" if they differ.
func commonExt(files []string) string {
	e := NormExt(files[0])
	for _, f := range files[1:] {
		if NormExt(f) != e {
			return ""
		}
	}
	return e
}

// ActionsFor returns the wheel entries that make sense for the selected files.
// Converting a file to its own format is never offered.
func ActionsFor(files []string) ([]Action, error) {
	k, err := SelectionKind(files)
	if err != nil {
		return nil, err
	}
	same := commonExt(files)
	withoutSame := func(list []Action) []Action {
		out := make([]Action, 0, len(list))
		for _, a := range list {
			if a.ID != "to:"+same {
				out = append(out, a)
			}
		}
		return out
	}

	var acts []Action
	switch k {
	case KindImage:
		acts = withoutSame(imageTargets)
		if len(files) > 1 {
			acts = append(acts, Action{ID: "img:pdf", Label: "One PDF", Desc: "Combine into one PDF"})
		} else {
			acts = append(acts, Action{ID: "img:pdf", Label: "PDF", Desc: "Convert to PDF"})
		}
		acts = append(acts,
			Action{ID: "img:half", Label: "50%", Desc: "Halve the size"},
			Action{ID: "img:compress", Label: "Compress", Desc: "Make the file smaller"},
		)
	case KindVideo:
		acts = withoutSame(videoTargets)
		acts = append(acts,
			Action{ID: "vid:gif", Label: "GIF", Desc: "Make an animated GIF"},
			Action{ID: "vid:mp3", Label: "MP3", Desc: "Extract the audio as MP3"},
			Action{ID: "vid:compress", Label: "Compress", Desc: "Make the video smaller"},
			Action{ID: "vid:mute", Label: "Mute", Desc: "Remove the audio"},
		)
	case KindAudio:
		acts = withoutSame(audioTargets)
	case KindPDF:
		acts = []Action{
			{ID: "pdf:png", Label: "PNG", Desc: "Every page to PNG"},
			{ID: "pdf:jpg", Label: "JPG", Desc: "Every page to JPG"},
		}
		if len(files) > 1 {
			acts = append(acts, Action{ID: "pdf:merge", Label: "Merge", Desc: "Merge into one PDF"})
		}
		// With a single PDF the user interface turns "Split" into a page-range
		// picker and runs "pdf:extract:A-B" or "pdf:split:A-B" instead.
		acts = append(acts, Action{ID: "pdf:split", Label: "Split", Desc: "One file per page"})
	}
	return acts, nil
}
