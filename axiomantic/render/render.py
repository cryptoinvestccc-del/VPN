#!/usr/bin/env python3
"""Renders the site's 3D pictures with Blender as a Python module.

    pip install bpy==5.2.2 pillow fonttools brotli         # Python 3.13, CPU only is fine
    python axiomantic/render/render.py brand --preview
    python axiomantic/render/render.py brand
    python axiomantic/render/render.py brand --shot hero-loop --frames 0:96
    python axiomantic/render/render.py brand --force       # re-render every loop frame

Each scene in scenes/ lists its shots. Renders land in render/out/ (not
in git); encode.py turns them into the AVIF/WebP/MP4 files the site
uses. The studio HDRIs come from Poly Haven (CC0) and are cached in
render/.cache/hdri on first use.

Loop frames already in out/<shot>/ are kept (long loops can be rendered
in pieces), but only while the scene, lib.py, the shot and the render
settings are the same as when they were made: the frames' fingerprint is
kept in out/<shot>/.fingerprint, and a changed fingerprint deletes them.
"""
import argparse
import glob
import hashlib
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

PH = 'https://dl.polyhaven.org/file/ph-assets/HDRIs/hdr/2k/'
# name: (url, sha256 of the file) — a cut-off or damaged download never
# passes for a cached HDRI
HDRIS = {
    'studio_small_09': (PH + 'studio_small_09_2k.hdr',
                        '36724313c0fc66dab126ff90f081b85a0b1ef47be65eda1417bd20b033f0573f'),
    'studio_small_03': (PH + 'studio_small_03_2k.hdr',
                        '156c2946a6cd0c1d8b662cd97e8a492436331ec2418a33901a324f4420618076'),
    'brown_photostudio_02': (PH + 'brown_photostudio_02_2k.hdr',
                             '153ca0cb899575c00f5b23065958d250a0f73e8789c8cf5957ccea313baf2bb6'),
    'photo_studio_loft_hall': (PH + 'photo_studio_loft_hall_2k.hdr',
                               '2eadabfce70a1f27d958aa18b2572d75883f9964f4656c10e706a44cafe129d0'),
}


def sha256(path):
    h = hashlib.sha256()
    with open(path, 'rb') as f:
        for block in iter(lambda: f.read(1 << 20), b''):
            h.update(block)
    return h.hexdigest()


def ensure_hdris():
    """Downloads each HDRI to <name>.hdr.part and moves it into the cache
    only once it is whole; a damaged cached file is downloaded again."""
    import lib
    os.makedirs(lib.HDRI_DIR, exist_ok=True)
    for name, (url, digest) in HDRIS.items():
        path = os.path.join(lib.HDRI_DIR, name + '.hdr')
        if os.path.exists(path):
            if sha256(path) == digest:
                continue
            print('damaged', path, '- downloading again')
        print('downloading', name)
        part = path + '.part'
        try:
            urllib.request.urlretrieve(url, part)  # raises if shorter than Content-Length
            if sha256(part) != digest:
                raise RuntimeError(f'{name}: downloaded file does not match its sha256')
            os.replace(part, path)
        finally:
            if os.path.exists(part):
                os.remove(part)


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


def fingerprint(scene, shot, w, h, samples):
    """What a loop frame depends on: the scene and lib.py code, the shot
    and the render settings."""
    import lib
    d = hashlib.sha256()
    for mod in (scene, lib):
        with open(mod.__file__, 'rb') as f:
            d.update(f.read())
    d.update(repr((sorted(shot.items()), w, h, samples)).encode())
    return d.hexdigest()


def frame_range(arg, total):
    try:
        a, b = (int(x) for x in arg.split(':'))
    except ValueError:
        sys.exit(f'--frames {arg}: expected a:b')
    if not 0 <= a < b <= total:
        sys.exit(f'--frames {arg}: the loop has {total} frames (0..{total - 1}), need 0 <= a < b <= {total}')
    return a, b


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('scene')
    ap.add_argument('--preview', action='store_true', help='quarter resolution, few samples')
    ap.add_argument('--shot', help='only this shot id')
    ap.add_argument('--frames', help='animation frames a:b for looping shots')
    ap.add_argument('--force', action='store_true', help='re-render loop frames that already exist')
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
                a, b = frame_range(args.frames, shot['frames'])
            else:
                a, b = 0, shot['frames']
            frames = range(a, b)
        import lib
        w, h = shot['size']
        samples = shot.get('samples', 256)
        if args.preview:
            w, h, samples = w // 4, h // 4, max(16, samples // 8)
        if shot.get('frames') and not args.preview:
            # frames left from another version of the scene would be
            # skipped below and end up in the video next to new ones
            loop_dir = os.path.join(args.out, shot['id'])
            stamp = os.path.join(loop_dir, '.fingerprint')
            current = fingerprint(scene, shot, w, h, samples)
            stored = None
            if os.path.exists(stamp):
                with open(stamp) as fh:
                    stored = fh.read().strip()
            stale = sorted(glob.glob(os.path.join(loop_dir, '*.png')))
            if stored != current and stale:
                print('scene changed since', loop_dir, 'was rendered: removing', len(stale), 'old frames')
                for old in stale:
                    os.remove(old)
            os.makedirs(loop_dir, exist_ok=True)
            with open(stamp, 'w') as fh:
                fh.write(current + '\n')
        built = None
        for f in frames:
            name = shot['id'] if f is None else f"{shot['id']}/{f:04d}"
            path = os.path.join(args.out, ('preview/' if args.preview else '') + name + '.png')
            if f is not None and os.path.exists(path) and not args.preview and not args.force:
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
