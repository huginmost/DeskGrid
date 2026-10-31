"""Render DeskGrid's own geometric artwork. Requires Pillow only to regenerate assets.

Normal builds use the checked-in PNG/ICO files and do not require Python/Pillow.
"""
from io import BytesIO
from pathlib import Path
import struct

from PIL import Image, ImageDraw, ImageFilter

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / 'internal/branding'
OUT.mkdir(parents=True, exist_ok=True)


def gradient(size, top, bottom):
    image = Image.new('RGBA', size)
    draw = ImageDraw.Draw(image)
    for y in range(size[1]):
        t = y / max(size[1] - 1, 1)
        color = tuple(round(a + (b - a) * t) for a, b in zip(top, bottom)) + (255,)
        draw.line((0, y, size[0], y), fill=color)
    return image


def rounded_layer(canvas, box, radius, top, bottom):
    x, y, right, low = box
    size = (right - x, low - y)
    mask = Image.new('L', size)
    ImageDraw.Draw(mask).rounded_rectangle((0, 0, size[0] - 1, size[1] - 1), radius, fill=255)
    canvas.paste(gradient(size, top, bottom), (x, y), mask)


def app_icon(size):
    image = Image.new('RGBA', (1024, 1024))
    shadow = Image.new('RGBA', image.size)
    ImageDraw.Draw(shadow).rounded_rectangle((88, 100, 936, 948), 208, fill=(4, 10, 26, 75))
    image.alpha_composite(shadow.filter(ImageFilter.GaussianBlur(18)))
    rounded_layer(image, (72, 64, 952, 944), 208, (31, 54, 91), (12, 23, 46))
    # Four panes share the same gutters, with a wider left column and taller lower row.
    panes = [((232, 224, 544, 424), (243, 249, 255), (211, 228, 255)),
             ((576, 224, 776, 424), (148, 191, 255), (115, 162, 242)),
             ((232, 456, 544, 768), (163, 196, 255), (111, 158, 238)),
             ((576, 456, 776, 768), (104, 240, 216), (44, 198, 184))]
    for box, top, bottom in panes:
        rounded_layer(image, box, 36, top, bottom)
    return image.resize((size, size), Image.Resampling.LANCZOS)


def tray_icon(size, dark):
    scale = 8
    image = Image.new('RGBA', (16 * scale, 16 * scale))
    draw = ImageDraw.Draw(image)
    color = '#edf4ff' if dark else '#243b60'
    accent = '#68e7d1' if dark else '#087e80'
    for box, fill in [((2, 2, 8, 6), color), ((10, 2, 14, 6), color),
                      ((2, 8, 8, 14), color), ((10, 8, 14, 14), accent)]:
        draw.rounded_rectangle(tuple(int(v * scale) for v in box), radius=scale, fill=fill)
    return image.resize((size, size), Image.Resampling.LANCZOS)


def png_bytes(image):
    stream = BytesIO()
    image.save(stream, format='PNG', optimize=True)
    return stream.getvalue()


app_icon(512).save(OUT / 'deskgrid.png')
sizes = (16, 20, 24, 32, 40, 48, 64, 128, 256)
images = [png_bytes(app_icon(size)) for size in sizes]
offset = 6 + 16 * len(images)
entries = []
for size, data in zip(sizes, images):
    entries.append(struct.pack('<BBBBHHII', size % 256, size % 256, 0, 0, 1, 32, len(data), offset))
    offset += len(data)
(OUT / 'deskgrid.ico').write_bytes(struct.pack('<HHH', 0, 1, len(images)) + b''.join(entries + images))
for dark in (False, True):
    tray_icon(32, dark).save(OUT / ('tray-dark.png' if dark else 'tray-light.png'))

(OUT / 'deskgrid.svg').write_text('''<svg xmlns="http://www.w3.org/2000/svg" width="512" height="512" viewBox="0 0 512 512">
  <title>DeskGrid</title>
  <defs>
    <linearGradient id="base" x2="0" y2="1"><stop stop-color="#1f365b"/><stop offset="1" stop-color="#0c172e"/></linearGradient>
    <linearGradient id="pale" x2="0" y2="1"><stop stop-color="#f3f9ff"/><stop offset="1" stop-color="#d3e4ff"/></linearGradient>
    <linearGradient id="blue" x2="0" y2="1"><stop stop-color="#94bfff"/><stop offset="1" stop-color="#73a2f2"/></linearGradient>
    <linearGradient id="wide" x2="0" y2="1"><stop stop-color="#a3c4ff"/><stop offset="1" stop-color="#6f9eee"/></linearGradient>
    <linearGradient id="mint" x2="0" y2="1"><stop stop-color="#68f0d8"/><stop offset="1" stop-color="#2cc6b8"/></linearGradient>
  </defs>
  <rect x="36" y="32" width="440" height="440" rx="104" fill="url(#base)"/>
  <rect x="116" y="112" width="156" height="100" rx="18" fill="url(#pale)"/>
  <rect x="288" y="112" width="100" height="100" rx="18" fill="url(#blue)"/>
  <rect x="116" y="228" width="156" height="156" rx="18" fill="url(#wide)"/>
  <rect x="288" y="228" width="100" height="156" rx="18" fill="url(#mint)"/>
</svg>
''', encoding='utf-8')
for dark in (False, True):
    body, accent = ('#edf4ff', '#68e7d1') if dark else ('#243b60', '#087e80')
    (OUT / ('tray-dark.svg' if dark else 'tray-light.svg')).write_text(f'''<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" viewBox="0 0 16 16">
  <title>DeskGrid tray</title>
  <g fill="{body}"><rect x="2" y="2" width="6" height="4" rx="1"/><rect x="10" y="2" width="4" height="4" rx="1"/><rect x="2" y="8" width="6" height="6" rx="1"/></g>
  <rect x="10" y="8" width="4" height="6" rx="1" fill="{accent}"/>
</svg>
''', encoding='utf-8')

# A documentation preview shows the actual rasters on both taskbar themes.
preview = Image.new('RGB', (720, 360), '#eef2f8')
preview.paste(app_icon(280), (28, 36), app_icon(280))
draw = ImageDraw.Draw(preview)
draw.rounded_rectangle((356, 56, 684, 156), 16, fill='#ffffff')
draw.rounded_rectangle((356, 188, 684, 288), 16, fill='#172133')
for dark, y in ((False, 88), (True, 220)):
    for x, size in ((388, 16), (452, 24), (524, 32), (600, 40)):
        icon = tray_icon(size, dark)
        preview.paste(icon, (x, y + (32 - size) // 2), icon)
(ROOT / 'docs').mkdir(exist_ok=True)
preview.save(ROOT / 'docs/deskgrid-icons.png')
print('Generated DeskGrid SVG, multi-size ICO, PNG and theme-aware tray assets.')
