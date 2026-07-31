#!/usr/bin/env python3
"""Extract raster gradient and plane path from Untitled.svg for Android assets."""
import base64
import pathlib
import re
import sys

svg_path = pathlib.Path(sys.argv[1])
out_dir = pathlib.Path(sys.argv[2])
svg = svg_path.read_text(encoding="utf-8")

m = re.search(r'href="data:image/png;base64,([^"]+)"', svg)
if not m:
    raise SystemExit("embedded png not found")
png = base64.b64decode(m.group(1))
bg_path = out_dir / "fromgram_icon_background.png"
bg_path.write_bytes(png)
print(f"wrote background png ({len(png)} bytes)")

paths = re.findall(r'<path[^>]+fill="#FFFFFF"[^>]*d="([^"]+)"', svg, flags=re.I)
if not paths:
    paths = re.findall(r'<path[^>]+d="([^"]+)"[^>]+fill="#FFFFFF"', svg, flags=re.I)
if not paths:
    raise SystemExit("white plane path not found")
plane = re.sub(r"\s+", " ", paths[0]).strip()
print(f"plane path chars: {len(plane)}")

drawable_dir = out_dir.parent / "drawable"
drawable_dir.mkdir(parents=True, exist_ok=True)
plane_xml = f'''<?xml version="1.0" encoding="utf-8"?>
<vector xmlns:android="http://schemas.android.com/apk/res/android"
    android:width="108dp"
    android:height="108dp"
    android:viewportWidth="1000"
    android:viewportHeight="1000">
    <path
        android:pathData="{plane}"
        android:fillColor="#FFFFFF"
        android:fillType="evenOdd" />
</vector>
'''
(drawable_dir / "fromgram_icon_plane.xml").write_text(plane_xml, encoding="utf-8")
print("wrote fromgram_icon_plane.xml")

inset_xml = '''<?xml version="1.0" encoding="utf-8"?>
<inset xmlns:android="http://schemas.android.com/apk/res/android"
    android:drawable="@drawable/fromgram_icon_plane"
    android:inset="18%" />
'''
(drawable_dir / "fromgram_icon_plane_inset.xml").write_text(inset_xml, encoding="utf-8")
print("wrote fromgram_icon_plane_inset.xml")

try:
    from PIL import Image

    im = Image.open(bg_path).convert("RGB")
    w, h = im.size
    top = im.getpixel((w // 2, h // 8))
    bottom = im.getpixel((w // 2, h * 7 // 8))
    print(f"gradient sample top={top} bottom={bottom}")
except Exception as exc:
    print("pillow sample skipped:", exc)
