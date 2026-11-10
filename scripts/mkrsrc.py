"""Generates rsrc_windows_amd64.syso (icon + manifest) for Mandarino.exe.

A .syso file is a COFF object with a .rsrc section; the Go linker embeds it in the exe
automatically when building for windows/amd64 (no extra tools needed).
Usage (from the repository root, needs Pillow): python3 scripts/mkrsrc.py
"""
import io
import struct
from PIL import Image, ImageDraw

SIZES = [256, 64, 48, 32, 16]


def draw_icon(size):
    s = 4  # draw 4x larger, then shrink: smooth edges
    W = size * s
    img = Image.new("RGBA", (W, W), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    k = W / 32
    def e(x0, y0, x1, y1, fill):
        d.ellipse([x0 * k, y0 * k, x1 * k, y1 * k], fill=fill)
    e(3, 5, 29, 31, (255, 140, 0, 255))        # fruit
    e(8, 10, 14, 16, (255, 180, 80, 255))      # highlight
    e(15, 1, 26, 8, (76, 175, 80, 255))        # leaf
    d.rectangle([15 * k, 4 * k, 17 * k, 8 * k], fill=(90, 60, 30, 255))  # stalk
    return img.resize((size, size), Image.LANCZOS)


def png_bytes(img):
    b = io.BytesIO()
    img.save(b, "PNG")
    return b.getvalue()


MANIFEST = b"""<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <assemblyIdentity version="1.0.0.0" processorArchitecture="amd64" name="Mandarino" type="win32"/>
  <description>Mandarino - offline file converter</description>
  <dependency>
    <dependentAssembly>
      <assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="6.0.0.0"
        processorArchitecture="*" publicKeyToken="6595b64144ccf1df" language="*"/>
    </dependentAssembly>
  </dependency>
  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
    <security><requestedPrivileges><requestedExecutionLevel level="asInvoker" uiAccess="false"/></requestedPrivileges></security>
  </trustInfo>
  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
    <application><supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/></application>
  </compatibility>
  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true/pm</dpiAware>
      <dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">PerMonitorV2</dpiAwareness>
    </windowsSettings>
  </application>
</assembly>
"""


def build():
    icons = [png_bytes(draw_icon(s)) for s in SIZES]
    group = struct.pack("<HHH", 0, 1, len(icons))
    for i, (s, data) in enumerate(zip(SIZES, icons)):
        dim = 0 if s >= 256 else s
        group += struct.pack("<BBBBHHIH", dim, dim, 0, 0, 1, 32, len(data), i + 1)

    # resources: type -> [(id, data)]  (3 = RT_ICON, 14 = RT_GROUP_ICON, 24 = RT_MANIFEST)
    res = {3: [(i + 1, d) for i, d in enumerate(icons)], 14: [(1, group)], 24: [(1, MANIFEST)]}
    lang = 0x0409

    # layout: directory tables and entries first, then the data
    types = sorted(res)
    dir_size = lambda n: 16 + 8 * n
    off = dir_size(len(types))
    type_dir_off = {}
    for t in types:
        type_dir_off[t] = off
        off += dir_size(len(res[t]))
    name_dir_off = {}
    for t in types:
        for rid, _ in res[t]:
            name_dir_off[(t, rid)] = off
            off += dir_size(1)
    data_entry_off = {}
    for t in types:
        for rid, _ in res[t]:
            data_entry_off[(t, rid)] = off
            off += 16
    data_off = {}
    for t in types:
        for rid, data in res[t]:
            off = (off + 7) & ~7
            data_off[(t, rid)] = off
            off += len(data)
    total = (off + 7) & ~7

    buf = bytearray(total)
    relocs = []
    def dir_hdr(at, n):
        struct.pack_into("<IIHHHH", buf, at, 0, 0, 0, 0, 0, n)
    dir_hdr(0, len(types))
    for i, t in enumerate(types):
        struct.pack_into("<II", buf, 16 + 8 * i, t, 0x80000000 | type_dir_off[t])
        dir_hdr(type_dir_off[t], len(res[t]))
        for j, (rid, data) in enumerate(res[t]):
            struct.pack_into("<II", buf, type_dir_off[t] + 16 + 8 * j, rid, 0x80000000 | name_dir_off[(t, rid)])
            nd = name_dir_off[(t, rid)]
            dir_hdr(nd, 1)
            struct.pack_into("<II", buf, nd + 16, lang, data_entry_off[(t, rid)])
            de = data_entry_off[(t, rid)]
            struct.pack_into("<IIII", buf, de, data_off[(t, rid)], len(data), 0, 0)
            relocs.append(de)  # OffsetToData is an RVA: the linker must relocate it
            buf[data_off[(t, rid)]:data_off[(t, rid)] + len(data)] = data

    # COFF object: file header, one section header, raw data, relocations, symbol table
    hdr_size = 20
    sec_size = 40
    raw_ptr = hdr_size + sec_size
    reloc_ptr = raw_ptr + total
    sym_ptr = reloc_ptr + 10 * len(relocs)
    out = bytearray()
    out += struct.pack("<HHIIIHH", 0x8664, 1, 0, sym_ptr, 1, 0, 0)
    out += struct.pack("<8sIIIIIIHHI", b".rsrc", 0, 0, total, raw_ptr, reloc_ptr, 0, len(relocs), 0, 0x40000040)
    out += buf
    for r in relocs:
        out += struct.pack("<IIH", r, 0, 3)  # IMAGE_REL_AMD64_ADDR32NB against symbol 0
    out += struct.pack("<8sIhHBB", b".rsrc", 0, 1, 0, 3, 0)  # section symbol
    out += struct.pack("<I", 4)  # empty string table
    open("rsrc_windows_amd64.syso", "wb").write(out)

    # also write a .ico and a .png for the docs and packaging
    ico = struct.pack("<HHH", 0, 1, len(icons))
    o = 6 + 16 * len(icons)
    for s, data in zip(SIZES, icons):
        dim = 0 if s >= 256 else s
        ico += struct.pack("<BBBBHHII", dim, dim, 0, 0, 1, 32, len(data), o)
        o += len(data)
    ico += b"".join(icons)
    open("packaging/windows/mandarino.ico", "wb").write(ico)
    draw_icon(256).save("docs/screenshots/icon.png")


if __name__ == "__main__":
    build()
