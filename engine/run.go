package engine

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// Progress is called while a job runs. index is the file being processed
// (starting at 0), total the number of files and frac how far along the
// current file is (0..1; it stays 0 when the tool cannot tell).
type Progress func(index, total int, frac float64)

// Failure describes one input that could not be converted.
type Failure struct {
	File string // empty when the failure concerns the whole job
	Err  error
}

// Result is the outcome of Run.
type Result struct {
	Outputs []string  // files that were created
	Failed  []Failure // inputs that could not be converted
}

// Run executes the action actionID (see ActionsFor) on files.
// A failure on one file does not stop the others. Cancelling ctx stops the
// current conversion (ffmpeg is killed) and skips the remaining files.
func Run(ctx context.Context, actionID string, files []string, prog Progress) Result {
	var res Result
	if prog == nil {
		prog = func(int, int, float64) {}
	}
	kind, err := SelectionKind(files)
	if err != nil {
		res.Failed = append(res.Failed, Failure{Err: err})
		return res
	}

	// Actions that turn all the inputs into a single output.
	switch actionID {
	case "img:pdf":
		out, err := imagesToPDF(ctx, files, func(i int) { prog(i, len(files), 0) })
		res.add(out, err)
		return res
	case "pdf:merge":
		prog(0, 1, 0)
		out, err := mergePDFs(files)
		res.add(out, err)
		return res
	}

	// Everything else works file by file.
	for i, f := range files {
		if ctx.Err() != nil {
			res.Failed = append(res.Failed, Failure{File: f, Err: ctx.Err()})
			continue
		}
		prog(i, len(files), 0)
		frac := func(x float64) { prog(i, len(files), x) }
		outs, err := runOne(ctx, actionID, kind, f, frac)
		for _, o := range outs {
			release(o)
		}
		if err != nil {
			res.Failed = append(res.Failed, Failure{File: f, Err: err})
			continue
		}
		res.Outputs = append(res.Outputs, outs...)
	}
	return res
}

// add records the result of a whole-selection action.
func (r *Result) add(out string, err error) {
	if out != "" {
		release(out)
	}
	if err != nil {
		if out != "" {
			os.Remove(out)
		}
		r.Failed = append(r.Failed, Failure{Err: err})
		return
	}
	r.Outputs = append(r.Outputs, out)
}

// runOne converts a single file. On error it removes any partial output.
func runOne(ctx context.Context, actionID string, kind Kind, in string, frac func(float64)) ([]string, error) {
	// single wraps functions that produce exactly one output file.
	single := func(out string, err error) ([]string, error) {
		if err != nil {
			if out != "" {
				os.Remove(out)
				release(out)
			}
			return nil, err
		}
		return []string{out}, nil
	}

	// media runs an ffmpeg action. ffmpeg writes to "<name> - in progress.<ext>"
	// and the file only gets its final name once it is complete. Opening a
	// half-written MP4 fails ("can't play this file"), so this keeps users
	// from ever seeing an unfinished file under its real name.
	media := func(action, out string) ([]string, error) {
		tmp := UniquePath(withSuffix(out, " - in progress", RawExt(out)))
		defer release(tmp)
		args, err := mediaArgs(ctx, action, in, tmp)
		if err == nil {
			err = runFFmpeg(ctx, args, frac)
		}
		if err == nil {
			err = os.Rename(tmp, out)
		}
		if err != nil {
			os.Remove(tmp)
		}
		return single(out, err)
	}

	// "to:<ext>" converts to another format of the same family.
	if ext, ok := strings.CutPrefix(actionID, "to:"); ok {
		switch kind {
		case KindImage:
			return single(convertImage(ctx, in, ext))
		case KindVideo:
			return media("v:"+ext, UniquePath(replaceExt(in, ext)))
		case KindAudio:
			return media("a:"+ext, UniquePath(replaceExt(in, ext)))
		}
		return nil, ErrUnsupported
	}

	switch actionID {
	case "img:half":
		return single(halveImage(ctx, in))
	case "img:compress":
		return single(compressImage(ctx, in))
	case "vid:gif":
		return media("v:gif", UniquePath(replaceExt(in, "gif")))
	case "vid:mp3":
		return media("a:mp3", UniquePath(replaceExt(in, "mp3")))
	case "vid:compress":
		return media("v:compress", UniquePath(withSuffix(in, " (compressed)", "mp4")))
	case "vid:mute":
		return media("v:mute", UniquePath(withSuffix(in, " (muted)", RawExt(in))))
	case "pdf:png":
		return cleanup(pdfToImages(ctx, in, "png", frac))
	case "pdf:jpg":
		return cleanup(pdfToImages(ctx, in, "jpg", frac))
	case "pdf:split":
		return cleanup(splitPDF(ctx, in, 0, 0, frac))
	}

	// Page ranges chosen in the "Split" picker: "pdf:split:3-7", "pdf:extract:3-7".
	if r, ok := strings.CutPrefix(actionID, "pdf:split:"); ok {
		from, to, err := parseRange(r)
		if err != nil {
			return nil, err
		}
		return cleanup(splitPDF(ctx, in, from, to, frac))
	}
	if r, ok := strings.CutPrefix(actionID, "pdf:extract:"); ok {
		from, to, err := parseRange(r)
		if err != nil {
			return nil, err
		}
		return single(extractPDF(in, from, to))
	}
	return nil, fmt.Errorf("unknown action: %s", actionID)
}

// cleanup deletes the pages already written when a multi-page job fails half way.
func cleanup(outs []string, err error) ([]string, error) {
	if err != nil {
		for _, o := range outs {
			os.Remove(o)
			release(o)
		}
		return nil, err
	}
	return outs, nil
}
