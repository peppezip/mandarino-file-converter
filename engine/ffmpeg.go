package engine

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// ToolDir is the folder where the bundled helpers live (ffmpeg and the pdfium
// library). The app sets it to the folder of its own executable.
var ToolDir string

// exeName adds ".exe" on Windows.
func exeName(n string) string {
	if isWindows {
		return n + ".exe"
	}
	return n
}

// FFmpegPath finds ffmpeg next to the program first (bundled copy), then in PATH.
func FFmpegPath() (string, error) {
	name := exeName("ffmpeg")
	if ToolDir != "" {
		for _, p := range []string{
			filepath.Join(ToolDir, name),
			filepath.Join(ToolDir, "bin", name),
			filepath.Join(ToolDir, "ffmpeg", name),
		} {
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p, nil
			}
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	return "", errors.New("ffmpeg not found: keep it in the same folder as Mandarino or install it")
}

// "Duration: 00:01:23.45" in ffmpeg's log tells us how long the input is.
var reDuration = regexp.MustCompile(`Duration: (\d+):(\d+):(\d+(?:\.\d+)?)`)

func hmsToSec(h, m, s string) float64 {
	hh, _ := strconv.ParseFloat(h, 64)
	mm, _ := strconv.ParseFloat(m, 64)
	ss, _ := strconv.ParseFloat(s, 64)
	return hh*3600 + mm*60 + ss
}

// runFFmpeg runs ffmpeg with args. onFrac (may be nil) receives the progress 0..1.
//
// Progress comes from "-progress pipe:1", which prints "out_time_us=..." lines
// on stdout; dividing by the input duration (read from stderr) gives a fraction.
// The last stderr lines are kept to build a readable error message.
func runFFmpeg(ctx context.Context, args []string, onFrac func(float64)) error {
	bin, err := FFmpegPath()
	if err != nil {
		return err
	}
	full := append([]string{"-hide_banner", "-nostdin", "-y", "-progress", "pipe:1", "-nostats"}, args...)
	cmd := exec.CommandContext(ctx, bin, full...)
	hideConsole(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not start ffmpeg: %w", err)
	}

	var (
		mu       sync.Mutex
		duration float64
		tail     []string
		wg       sync.WaitGroup
	)
	wg.Add(2)
	go func() { // stderr: duration + last lines for error messages
		defer wg.Done()
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			mu.Lock()
			if duration == 0 {
				if m := reDuration.FindStringSubmatch(line); m != nil {
					duration = hmsToSec(m[1], m[2], m[3])
				}
			}
			tail = append(tail, line)
			if len(tail) > 30 {
				tail = tail[1:]
			}
			mu.Unlock()
		}
	}()
	go func() { // stdout: progress reports
		defer wg.Done()
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			v, ok := strings.CutPrefix(sc.Text(), "out_time_us=")
			if !ok || onFrac == nil {
				continue
			}
			us, err := strconv.ParseFloat(v, 64)
			mu.Lock()
			d := duration
			mu.Unlock()
			if err == nil && d > 0 {
				onFrac(math.Max(0, math.Min(1, us/1e6/d)))
			}
		}
		_, _ = io.Copy(io.Discard, stdout)
	}()
	wg.Wait()

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err() // cancelled by the user
		}
		mu.Lock()
		msg := lastMeaningful(tail)
		mu.Unlock()
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("ffmpeg: %s", msg)
	}
	return nil
}

// lastMeaningful picks the most useful line from the end of ffmpeg's log
// (skipping generic lines such as "Conversion failed!").
func lastMeaningful(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" || strings.HasPrefix(l, "Conversion failed") || strings.HasPrefix(l, "[out#") && strings.Contains(l, "Error") {
			continue
		}
		return l
	}
	return ""
}

// mediaInfo holds what we need to know about an input video.
type mediaInfo struct {
	fps  float64 // declared frames per second (0 if unknown)
	w, h int     // size in pixels (0 if unknown)
}

var (
	reVideoLine = regexp.MustCompile(`Stream #\d+:\d+.*: Video: .*`)
	reFPS       = regexp.MustCompile(`([\d.]+) fps`)
	reTBR       = regexp.MustCompile(`([\d.]+)k? tbr`)
	reSize      = regexp.MustCompile(`, (\d{2,5})x(\d{2,5})`)
)

// probe reads the first video stream's properties with "ffmpeg -i <file>".
// (Only ffmpeg is bundled, not ffprobe, so we parse its log.)
func probe(ctx context.Context, in string) mediaInfo {
	var info mediaInfo
	bin, err := FFmpegPath()
	if err != nil {
		return info
	}
	cmd := exec.CommandContext(ctx, bin, "-hide_banner", "-nostdin", "-i", in)
	hideConsole(cmd)
	out, _ := cmd.CombinedOutput() // exits with an error because no output is given: expected
	line := reVideoLine.FindString(string(out))
	if line == "" {
		return info
	}
	if m := reFPS.FindStringSubmatch(line); m != nil {
		info.fps, _ = strconv.ParseFloat(m[1], 64)
	} else if m := reTBR.FindStringSubmatch(line); m != nil && !strings.Contains(m[0], "k") {
		info.fps, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reSize.FindStringSubmatch(line); m != nil {
		info.w, _ = strconv.Atoi(m[1])
		info.h, _ = strconv.Atoi(m[2])
	}
	return info
}

// targetFPS picks a constant, "normal" frame rate for H.264 output.
//
// Screen recordings (Windows Snipping Tool, Xbox Game Bar, OBS...) use a
// variable frame rate and sometimes declare absurd values such as 1000 fps.
// Copying that into an MP4 produces files some players refuse to open, so:
// unknown -> 30, above 60 -> 60, NTSC rates kept exact, anything else rounded.
func targetFPS(fps float64) string {
	switch {
	case fps <= 1 || fps > 240:
		return "30"
	case fps > 60:
		return "60"
	}
	for _, std := range []struct {
		v float64
		s string
	}{{23.976, "24000/1001"}, {29.97, "30000/1001"}, {59.94, "60000/1001"}} {
		if math.Abs(fps-std.v) < 0.02 {
			return std.s
		}
	}
	return strconv.Itoa(max(10, int(math.Round(fps))))
}

// h264 returns options for an H.264 video that every player accepts
// (Windows Media Player, phones, TVs, browsers): constant frame rate,
// even width and height, yuv420p colours and a single stereo AAC track.
// maxW > 0 also limits the width (used by "Compress").
func h264(info mediaInfo, crf string, maxW int, audioRate string) []string {
	// out_range=tv: screen recordings and phone videos are often "full range"
	// (yuvj420p). Recent ffmpeg versions keep that range unless asked, and some
	// players show such files with wrong colours or refuse them, so the output
	// is always converted to the standard "limited" range.
	scale := "scale='trunc(iw/2)*2':'trunc(ih/2)*2':out_range=tv"
	if maxW > 0 {
		scale = fmt.Sprintf("scale='trunc(min(%d,iw)/2)*2':-2:out_range=tv", maxW)
	}
	return []string{
		"-map", "0:v:0", "-map", "0:a:0?", "-map_metadata", "0",
		"-vf", scale + ",format=yuv420p", "-color_range", "tv",
		"-fps_mode", "cfr", "-r", targetFPS(info.fps),
		"-c:v", "libx264", "-preset", "medium", "-crf", crf, "-profile:v", "high",
		"-c:a", "aac", "-b:a", audioRate, "-ac", "2", "-ar", "48000",
		"-max_muxing_queue_size", "4096",
	}
}

// mediaArgs returns the ffmpeg arguments for a video ("v:...") or audio ("a:...") action.
func mediaArgs(ctx context.Context, action, in, out string) ([]string, error) {
	i := []string{"-i", in}
	switch action {
	// ---- video ----
	case "v:mp4":
		return cat(i, h264(probe(ctx, in), "20", 0, "192k"), []string{"-movflags", "+faststart", "-f", "mp4", out}), nil
	case "v:mov":
		return cat(i, h264(probe(ctx, in), "20", 0, "192k"), []string{"-f", "mov", out}), nil
	case "v:mkv":
		// MKV accepts almost any codec: copy the streams without re-encoding (fast, lossless).
		return cat(i, []string{"-map", "0:v", "-map", "0:a?", "-map_metadata", "0", "-c", "copy", out}), nil
	case "v:webm":
		return cat(i, []string{"-map", "0:v:0", "-map", "0:a?", "-c:v", "libvpx-vp9", "-crf", "32", "-b:v", "0",
			"-row-mt", "1", "-deadline", "good", "-cpu-used", "4", "-c:a", "libopus", "-b:a", "128k", out}), nil
	case "v:avi":
		return cat(i, []string{"-map", "0:v:0", "-map", "0:a?", "-c:v", "mpeg4", "-vtag", "xvid", "-q:v", "3",
			"-c:a", "libmp3lame", "-q:a", "3", out}), nil
	case "v:gif":
		// 12 fps, at most 640 px wide, with an optimized 256-colour palette.
		return cat(i, []string{"-vf", "fps=12,scale='min(640,iw)':-2:flags=lanczos,split[a][b];[a]palettegen=stats_mode=diff[p];[b][p]paletteuse=dither=bayer:bayer_scale=4",
			"-loop", "0", out}), nil
	case "v:compress":
		// CRF 28 and at most 1920 px wide: usually 3-10x smaller with little visible loss.
		return cat(i, h264(probe(ctx, in), "28", 1920, "128k"), []string{"-movflags", "+faststart", "-f", "mp4", out}), nil
	case "v:mute":
		return cat(i, []string{"-map", "0:v", "-map_metadata", "0", "-c", "copy", "-an", out}), nil

	// ---- audio (also used to extract the soundtrack of a video) ----
	case "a:mp3":
		return cat(i, []string{"-vn", "-map_metadata", "0", "-c:a", "libmp3lame", "-q:a", "2", out}), nil
	case "a:wav":
		return cat(i, []string{"-vn", "-c:a", "pcm_s16le", out}), nil
	case "a:flac":
		return cat(i, []string{"-vn", "-map_metadata", "0", "-c:a", "flac", out}), nil
	case "a:m4a":
		return cat(i, []string{"-vn", "-map_metadata", "0", "-c:a", "aac", "-b:a", "256k", "-movflags", "+faststart", out}), nil
	case "a:ogg":
		return cat(i, []string{"-vn", "-map_metadata", "0", "-c:a", "libvorbis", "-q:a", "6", out}), nil
	case "a:opus":
		return cat(i, []string{"-vn", "-map_metadata", "0", "-c:a", "libopus", "-b:a", "160k", out}), nil
	}
	return nil, fmt.Errorf("unknown action: %s", action)
}

// cat concatenates argument lists.
func cat(parts ...[]string) []string {
	var out []string
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}
