MANDARINO - offline file converter for Windows
==============================================

INSTALL
1. Right-click the zip > "Extract All...". Pick a folder that will stay put,
   for example Documents\Mandarino. Do not run it from inside the zip.
2. Double-click Mandarino.exe.
   If "Windows protected your PC" appears, click "More info" > "Run anyway".
   This happens with every program that is not code-signed.
3. An orange icon appears next to the clock: Mandarino is running.
   From now on it starts with Windows (untick "Start with Windows" in the
   icon's menu to stop that).

USE
In File Explorer (or on the desktop) start dragging one or more files and,
while still holding the mouse button:
  - press Shift to CONVERT them (the wheel shows the formats);
  - press Ctrl for ACTIONS: Bluetooth, Copy, Path, Move to...
Move onto a slice (it lights up) and release the mouse button.
Press the other key to switch wheel; release elsewhere for a normal
drag & drop; Esc cancels. Near the edges of the screen the wheel moves
inwards so it is never cut.

Move to...: rest on it and the wheel shows your folders (Desktop,
Documents, Downloads, drives...). Rest on a folder to open it, "Up" goes
back, "More..." shows more folders. Drop on a folder, or on "Move here" in
the middle.
Bluetooth: opens the Windows Bluetooth transfer, which asks for the device
(or the Bluetooth settings if Bluetooth is not available).
Copy: the files go to the clipboard, paste them with Ctrl+V anywhere.
Path: the full path goes to the clipboard.

Conversions never touch the originals: the new file appears next to them.
If a conversion takes a while, a smaller wheel shows the progress:
drag it anywhere, the x cancels, a right click hides it.
Nothing is uploaded: everything happens on your PC.

WHAT IT CONVERTS
- Images (JPG, PNG, WebP, BMP, TIFF, GIF, ICO, HEIC, AVIF):
  to JPG, PNG, WebP, BMP, TIFF, GIF, ICO, PDF; "One PDF" for several images;
  50% (half the size); Compress.
- Video (MP4, MOV, MKV, WebM, AVI, WMV, FLV, MPG, 3GP, TS, MTS):
  to MP4, MKV, MOV, WebM, AVI; animated GIF; extract MP3; Compress; Mute.
- Audio (MP3, WAV, FLAC, M4A, AAC, OGG, Opus, WMA, AIFF, AMR):
  to MP3, WAV, FLAC, M4A, OGG, Opus.
- PDF: pages to PNG or JPG; Merge (several PDFs); Split: with one PDF the
  wheel asks "from page / to page" (click, mouse wheel or type the
  numbers), then Extract (one PDF) or One per page.

UNINSTALL
Icon menu > untick "Start with Windows" > Quit. Then delete the folder.

FILES (keep them together)
- Mandarino.exe   the program
- ffmpeg.exe      audio/video/image engine (FFmpeg, GPL)
- pdfium.dll      PDF engine (PDFium, BSD/Apache)
- licenses\       licenses of Mandarino and of the bundled components
