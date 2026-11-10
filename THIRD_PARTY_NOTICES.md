# Third-party components

Mandarino's own source code is released under the MIT license (see `LICENSE`).
It has **no Go dependencies** besides the standard library. The release
archives also bundle two independent programs/libraries, which Mandarino runs
or loads at run time. They are not part of this repository and keep their own
licenses.

## FFmpeg

- What: `ffmpeg.exe` (Windows) / `ffmpeg` (Linux), used for audio, video and some image formats.
- Builds: by default <https://github.com/BtbN/FFmpeg-Builds> ("gpl" variant, static, downloaded by
  `scripts/fetch-deps.sh`). Other static GPL builds work too, for example
  <https://www.gyan.dev/ffmpeg/builds/> (Windows) or <https://johnvansickle.com/ffmpeg/> (Linux).
- License: **GNU GPL v3** for these builds (they include x264, x265, libvpx...).
  See <https://ffmpeg.org/legal.html>.
- Source code: <https://ffmpeg.org/download.html> (the pages of the builds above link the
  exact sources and configuration of every build).

Mandarino only runs `ffmpeg` as a separate program; it does not link to it.
If you redistribute a release archive, you redistribute FFmpeg too, so you must
also comply with the GPL (keeping this notice and pointing to the sources is enough
for unmodified binaries).

## PDFium

- What: `pdfium.dll` (Windows) / `libpdfium.so` (Linux), used for every PDF feature.
- Builds: <https://github.com/bblanchon/pdfium-binaries>
- License: BSD-3-Clause (PDFium) and Apache-2.0 (some components); the full texts
  are included in the release archives (`licenses/PDFium-LICENSE.txt`).
- Source code: <https://pdfium.googlesource.com/pdfium/>

## System libraries (Linux)

The Linux build links dynamically to libX11, libXext and cairo, which are
provided by the operating system (MIT and LGPL-2.1/MPL-1.1 licenses respectively).
