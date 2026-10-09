"""Post-processing for renders: put a transparent render on the page colour
and let the bright core bloom, the way a camera lens would.

    python post.py out/hero.png out/hero-navy.png --bg '#070A2A' --bloom 0.7

Done here rather than in Blender's compositor so that the poster and
every video frame get exactly the same treatment, and so that the flat
page colour matches the CSS byte for byte.
"""
import argparse

import numpy as np
from PIL import Image, ImageFilter


def to_linear(c):
    return np.where(c <= 0.04045, c / 12.92, ((c + 0.055) / 1.055) ** 2.4)


def to_srgb(c):
    c = np.clip(c, 0, 1)
    return np.where(c <= 0.0031308, c * 12.92, 1.055 * np.power(c, 1 / 2.4) - 0.055)


def hex_rgb(h):
    h = h.lstrip('#')
    return np.array([int(h[i:i + 2], 16) / 255 for i in (0, 2, 4)], dtype=np.float32)


def _blur(lin, radius):
    """Gaussian blur of a linear float image through 16-bit-ish PIL channels."""
    out = np.empty_like(lin)
    peak = max(float(lin.max()), 1e-6)
    for ch in range(3):
        img = Image.fromarray(np.uint8(np.clip(lin[..., ch] / peak, 0, 1) * 255), 'L')
        out[..., ch] = np.asarray(img.filter(ImageFilter.GaussianBlur(radius)), dtype=np.float32) / 255 * peak
    return out


def despeckle(a, below=0.6, size=5, guard=9):
    """Cycles denoises colour but not alpha, so a shadow-catcher floor keeps
    single dark grains. On the floor (alpha below `below`, and more than a
    few pixels away from any solid object, so silhouettes and thin wires keep
    their antialiasing) a grain that stands above its neighbourhood is cut
    down to the local median."""
    img = Image.fromarray(np.uint8(np.round(a[..., 0] * 255)), 'L')
    med = np.asarray(img.filter(ImageFilter.MedianFilter(size)), dtype=np.float32)[..., None] / 255.0
    solid = Image.fromarray(np.uint8(a[..., 0] > 0.9) * 255, 'L').filter(ImageFilter.MaxFilter(guard))
    near = (np.asarray(solid) > 0)[..., None]
    return np.where((a < below) & ~near, np.minimum(a, med), a)


def process(src, dst=None, bg=None, bloom=0.0, threshold=0.7, radius=0.035, vignette=0.0, quality=92,
            clean_floor=False):
    im = Image.open(src).convert('RGBA')
    arr = np.asarray(im, dtype=np.float32) / 255.0
    rgb, a = to_linear(arr[..., :3]), arr[..., 3:4]
    if clean_floor:
        a = despeckle(a)
    h, w = a.shape[:2]
    if bg is not None:
        base = to_linear(hex_rgb(bg))[None, None, :]
        rgb = rgb * a + base * (1 - a)
        a = np.ones_like(a)
    if bloom:
        lum = rgb @ np.array([0.2126, 0.7152, 0.0722], dtype=np.float32)
        bright = rgb * np.clip((lum - threshold) / (1 - threshold + 1e-6), 0, 1)[..., None]
        r = radius * max(w, h)
        glow = 0.55 * _blur(bright, r * 0.35) + 0.3 * _blur(bright, r) + 0.15 * _blur(bright, r * 2.5)
        rgb = rgb + glow * bloom
        if bg is None:
            a = np.clip(a + (glow.max(axis=2, keepdims=True) * bloom), 0, 1)
    if vignette:
        yy, xx = np.mgrid[0:h, 0:w].astype(np.float32)
        d = np.sqrt(((xx - w / 2) / (w / 2)) ** 2 + ((yy - h / 2) / (h / 2)) ** 2)
        rgb = rgb * (1 - vignette * np.clip(d - 0.55, 0, 1)[..., None])
    out = np.concatenate([to_srgb(rgb), a], axis=2)
    img = Image.fromarray(np.uint8(np.round(out * 255)), 'RGBA')
    if bg is not None:
        img = img.convert('RGB')
    if dst:
        if dst.endswith('.jpg'):
            img.convert('RGB').save(dst, quality=quality, optimize=True)
        else:
            img.save(dst)
    return img


if __name__ == '__main__':
    ap = argparse.ArgumentParser()
    ap.add_argument('src')
    ap.add_argument('dst')
    ap.add_argument('--bg')
    ap.add_argument('--bloom', type=float, default=0.0)
    ap.add_argument('--threshold', type=float, default=0.7)
    ap.add_argument('--radius', type=float, default=0.035)
    ap.add_argument('--vignette', type=float, default=0.0)
    a = ap.parse_args()
    process(a.src, a.dst, a.bg, a.bloom, a.threshold, a.radius, a.vignette)
