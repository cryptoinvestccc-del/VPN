#!/usr/bin/env python3
"""Renders the site's 3D pictures with Blender as a Python module.

    pip install bpy==5.2.2 pillow fonttools brotli         # Python 3.13, CPU only is fine
    python axiomantic/render/render.py brand --preview
    python axiomantic/render/render.py brand
    python axiomantic/render/render.py brand --shot hero-loop --frames 0:96

Each scene in scenes/ lists its shots. Renders land in render/out/ (not
in git); encode.py turns them into the AVIF/WebP/MP4 files the site
uses. The studio HDRIs come from Poly Haven (CC0) and are cached in
render/.cache/hdri on first use.
"""
import argparse
import importlib
import importlib.util
import os
import sys
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))

# Blender finds its colour management (AgX) only through OCIO when it runs
# as a module; it has to be set before bpy is imported.
_spec = importlib.util.find_spec('bpy')
if _spec and _spec.origin and 'OCIO' not in os.environ:
    root = os.path.dirname(_spec.origin)
    for ver in sorted(os.listdir(root), reverse=True):
        cfg = os.path.join(root, ver, 'datafiles', 'colormanagement', 'config.ocio')
        if os.path.exists(cfg):
            os.environ['OCIO'] = cfg
            break

sys.path.insert(0, HERE)
sys.dont_write_bytecode = True

HDRIS = {
    'studio_small_09': 'https://dl.polyhaven.org/file/ph-assets/HDRIs/hdr/2k/studio_small_09_2k.hdr',
    'studio_small_03': 'https://dl.polyhaven.org/file/ph-assets/HDRIs/hdr/2k/studio_small_03_2k.hdr',
    'brown_photostudio_02': 'https://dl.polyhaven.org/file/ph-assets/HDRIs/hdr/2k/brown_photostudio_02_2k.hdr',
    'photo_studio_loft_hall': 'https://dl.polyhaven.org/file/ph-assets/HDRIs/hdr/2k/photo_studio_loft_hall_2k.hdr',
}


def ensure_hdris():
    import lib
    os.makedirs(lib.HDRI_DIR, exist_ok=True)
    for name, url in HDRIS.items():
        path = os.path.join(lib.HDRI_DIR, name + '.hdr')
        if not os.path.exists(path):
            print('downloading', name)
            urllib.request.urlretrieve(url, path)


FONTS_SRC = os.path.join(HERE, '..', 'site', 'assets', 'fonts')


def ensure_font(family, weight):
    """A static TTF cut from the site's own variable woff2 (Blender needs TTF)."""
    import lib
    out = os.path.join(lib.FONT_DIR, f'{family}-{weight}.ttf')
    if os.path.exists(out):
        return out
    from fontTools.ttLib import TTFont
    from fontTools.varLib import instancer
    os.makedirs(lib.FONT_DIR, exist_ok=True)
    src = TTFont(os.path.join(FONTS_SRC, f'{family}-cyrillic-wght-normal.woff2'))
    static = instancer.instantiateVariableFont(src, {'wght': weight})
    static.flavor = None
    static.save(out)
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('scene')
    ap.add_argument('--preview', action='store_true', help='quarter resolution, few samples')
    ap.add_argument('--shot', help='only this shot id')
    ap.add_argument('--frames', help='animation frames a:b for looping shots')
    ap.add_argument('--out', default=os.path.join(HERE, 'out'))
    args = ap.parse_args()
    ensure_hdris()
    for weight in (700, 800):
        ensure_font('manrope', weight)
    scene = importlib.import_module('scenes.' + args.scene)
    for shot in scene.SHOTS:
        if args.shot and shot['id'] != args.shot:
            continue
        frames = [None]
        if shot.get('frames'):
            if args.frames:
                a, b = (int(x) for x in args.frames.split(':'))
            else:
                a, b = 0, shot['frames']
            frames = range(a, b)
        import lib
        w, h = shot['size']
        samples = shot.get('samples', 256)
        if args.preview:
            w, h, samples = w // 4, h // 4, max(16, samples // 8)
        built = None
        for f in frames:
            name = shot['id'] if f is None else f"{shot['id']}/{f:04d}"
            path = os.path.join(args.out, ('preview/' if args.preview else '') + name + '.png')
            if f is not None and os.path.exists(path) and not args.preview:
                continue  # resumable: long loops can be rendered in pieces
            phase = None if f is None else f / shot['frames']
            if built is not None and hasattr(scene, 'pose'):
                scene.pose(built, phase)  # same geometry, next point of the loop
            else:
                built = scene.build(shot, phase=phase)
            lib.render(path, w, h, samples)
            print('rendered', path, flush=True)


if __name__ == '__main__':
    main()
