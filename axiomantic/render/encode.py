#!/usr/bin/env python3
"""Turns renders into the files the site serves.

    python encode.py                 # every shot that has a render in out/
    python encode.py --only hero     # one shot

Stills become AVIF and WebP at several widths, recorded in
site/assets/img/manifest.json so that build.py can write <picture> tags
with exact width/height (no layout shift). Looping shots become an H.264
MP4 and a VP9 WebM, each in a desktop and a phone size.

Post-processing per shot (page colour, bloom) is taken from the shot's
'post' entry in its scene module, so the poster and every video frame
match the CSS colour behind them.
"""
import argparse
import glob
import importlib
import json
import os
import shutil
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
sys.dont_write_bytecode = True
OUT = os.path.join(HERE, 'out')
SITE = os.path.join(HERE, '..', 'site', 'assets')
IMG = os.path.join(SITE, 'img')
VIDEO = os.path.join(SITE, 'video')
MANIFEST = os.path.join(IMG, 'manifest.json')

from PIL import Image  # noqa: E402

import post  # noqa: E402

AVIF_Q = 58
WEBP_Q = 80


def shots():
    for path in sorted(glob.glob(os.path.join(HERE, 'scenes', '*.py'))):
        name = os.path.splitext(os.path.basename(path))[0]
        mod = importlib.import_module('scenes.' + name)
        for shot in mod.SHOTS:
            yield shot


def finished(shot):
    """The render with its post-processing applied, as a PIL image."""
    src = os.path.join(OUT, shot['id'] + '.png')
    p = shot.get('post', {})
    return post.process(src, bg=p.get('bg'), bloom=p.get('bloom', 0), threshold=p.get('threshold', 0.7),
                        radius=p.get('radius', 0.035), vignette=p.get('vignette', 0),
                        clean_floor=p.get('clean_floor', False))


def encode_still(shot, manifest):
    img = finished(shot)
    alpha = img.mode == 'RGBA'
    W, H = img.size
    widths = sorted({min(w, W) for w in shot.get('widths', [480, 800, 1200, 1600])})
    entry = {'w': W, 'h': H, 'alpha': alpha, 'avif': [], 'webp': []}
    os.makedirs(IMG, exist_ok=True)
    for w in widths:
        h = round(H * w / W)
        im = img if w == W else img.resize((w, h), Image.LANCZOS)
        for fmt, q in (('avif', AVIF_Q), ('webp', WEBP_Q)):
            name = f"{shot['id']}-{w}.{fmt}"
            kw = {'quality': q}
            if fmt == 'webp':
                kw['method'] = 6
            else:
                kw['speed'] = 4
            im.save(os.path.join(IMG, name), fmt.upper(), **kw)
            entry[fmt].append([w, 'assets/img/' + name])
    manifest[shot['id']] = entry
    print('still', shot['id'], widths)


def encode_loop(shot):
    frames = sorted(glob.glob(os.path.join(OUT, shot['id'], '*.png')))
    if len(frames) < shot['frames']:
        print('loop', shot['id'], f'only {len(frames)}/{shot["frames"]} frames, skipped')
        return
    p = shot.get('post', {})
    os.makedirs(VIDEO, exist_ok=True)
    with tempfile.TemporaryDirectory() as tmp:
        for i, f in enumerate(frames):
            post.process(f, os.path.join(tmp, f'{i:04d}.png'), bg=p.get('bg'), bloom=p.get('bloom', 0),
                         threshold=p.get('threshold', 0.7), radius=p.get('radius', 0.035))
        fps = str(shot.get('fps', 24))
        for size in shot.get('video_sizes', [1080, 720]):
            scale = f'scale={size}:{size}:flags=lanczos'
            base = os.path.join(VIDEO, f"{shot['id']}-{size}")
            subprocess.run(['ffmpeg', '-y', '-loglevel', 'error', '-framerate', fps, '-i', os.path.join(tmp, '%04d.png'),
                            '-vf', scale, '-c:v', 'libx264', '-preset', 'veryslow', '-crf', '23', '-pix_fmt', 'yuv420p',
                            '-profile:v', 'high', '-movflags', '+faststart', '-an', base + '.mp4'], check=True)
            subprocess.run(['ffmpeg', '-y', '-loglevel', 'error', '-framerate', fps, '-i', os.path.join(tmp, '%04d.png'),
                            '-vf', scale, '-c:v', 'libvpx-vp9', '-crf', '36', '-b:v', '0', '-row-mt', '1',
                            '-pix_fmt', 'yuv420p', '-an', base + '.webm'], check=True)
            print('loop', shot['id'], size, os.path.getsize(base + '.mp4') // 1024, 'KB mp4,',
                  os.path.getsize(base + '.webm') // 1024, 'KB webm')


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--only')
    args = ap.parse_args()
    if not shutil.which('ffmpeg'):
        print('ffmpeg not found: loops will be skipped')
    manifest = {}
    if os.path.exists(MANIFEST):
        with open(MANIFEST) as f:
            manifest = json.load(f)
    for shot in shots():
        if args.only and shot['id'] != args.only:
            continue
        if shot.get('frames'):
            if shutil.which('ffmpeg'):
                encode_loop(shot)
        elif os.path.exists(os.path.join(OUT, shot['id'] + '.png')):
            encode_still(shot, manifest)
    with open(MANIFEST, 'w') as f:
        json.dump(dict(sorted(manifest.items())), f, indent=1)
        f.write('\n')


if __name__ == '__main__':
    main()
