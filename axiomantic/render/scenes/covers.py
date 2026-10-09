"""Blog covers, one per category, in the house materials.

cover-marketing  target / audience: concentric ceramic and glass rings
                 around a glowing lilac core, seen at three quarters
cover-design     composition: a ceramic rounded square, a brand-blue
                 sphere and a frosted glass triangle as a still life
cover-dev        structure: a grid of lacquer and chrome blocks, a few
                 lit like active lines of code, on navy
cover-ai         AI as a helper: a luminous core inside a clear sphere
                 with a thin chrome orbit, on deep navy

All are opaque (the card has no background of its own): light covers are
rendered transparent over a shadow catcher and composited on their
colour; dark covers refract their own colour.
"""
import math

import lib

SHOTS = [
    {'id': 'cover-marketing', 'size': (1600, 1000), 'samples': 128,
     'post': {'bg': '#ECEAFF'}, 'widths': [480, 800, 1200, 1600]},
    {'id': 'cover-design', 'size': (1600, 1000), 'samples': 128,
     'post': {'bg': '#E6EEFF'}, 'widths': [480, 800, 1200, 1600]},
    {'id': 'cover-dev', 'size': (1600, 1000), 'samples': 128,
     'post': {'bg': '#0E1240', 'bloom': 0.35}, 'widths': [480, 800, 1200, 1600]},
    {'id': 'cover-ai', 'size': (1600, 1000), 'samples': 128,
     'post': {'bg': '#141A5C', 'bloom': 0.5}, 'widths': [480, 800, 1200, 1600]},
]


def light_page(bg):
    lib.world('studio_small_03', strength=0.8, rotation=-30, refract=bg, refract_mix=0.55,
              diffuse='#FFFFFF', diffuse_strength=0.7)


def build_marketing(shot):
    bg = '#ECEAFF'
    light_page(bg)
    tilt = (math.radians(62), 0, math.radians(-18))
    mats = [lib.ceramic('ring-white', roughness=0.3, coat=0.6),
            lib.frosted('ring-glass', tint='#D4CEFF', roughness=0.2),
            lib.ceramic('ring-white-2', roughness=0.3, coat=0.6)]
    for i, (R, r) in enumerate([(1.85, 0.13), (1.25, 0.12), (0.7, 0.11)]):
        lib.torus(f'ring{i}', R=R, r=r, loc=(0, 0, 1.0 + 0.08 * i), rot=tilt, mat=mats[i])
    lib.sphere('core', r=0.28, loc=(0, 0, 1.24), mat=lib.glow('core', inner='#F6F4FF', outer=lib.VIOLET, strength=3.0))
    lib.shadow_catcher(z=0.0)
    lib.camera(loc=(0.4, -8.4, 3.0), target=(0, 0, 1.15), lens=46)
    lib.light_rig(target=(0, 0, 1.0))


def build_design(shot):
    bg = '#E6EEFF'
    light_page(bg)
    lib.rounded_box('square', size=(1.3, 1.3, 1.3), radius=0.18, loc=(-1.0, 0.3, 0.65),
                    rot=(0, 0, math.radians(24)), mat=lib.ceramic('square', color='#FFFFFF', roughness=0.3, coat=0.6),
                    segments=12)
    lib.sphere('ball', r=0.62, loc=(0.75, -0.35, 0.62), mat=lib.ceramic('ball', color=lib.BLUE, roughness=0.18, coat=1.0))
    tri = lib.triangle_ring('tri', R=0.78, tube=0.1, corner=0.18, loc=(0.25, 0.75, 1.55),
                            rot=(math.radians(84), 0, math.radians(-12)),
                            mat=lib.frosted('tri-glass', tint='#CFC8FF', roughness=0.16))
    tri.name = 'tri'
    lib.shadow_catcher(z=0.0)
    lib.camera(loc=(0.2, -7.6, 2.4), target=(0, 0.1, 0.95), lens=52)
    lib.light_rig(target=(0, 0, 0.8))


def build_dev(shot):
    bg = '#0E1240'
    lib.world('studio_small_09', strength=0.3, rotation=30, refract=bg, refract_mix=0.8)
    lac = lib.lacquer('block', color='#110D33', metallic=0.4, roughness=0.24)
    chrome = lib.chrome('block-chrome', roughness=0.14)
    lit = lib.glow('block-lit', inner='#E9E6FF', outer=lib.VIOLET, strength=1.6)
    # rows of "code": blocks of different lengths, a few chrome, a few lit
    rows = [
        [1.6, 0.8, 1.2],
        [0.6, 2.0, 0.9],
        [1.1, 0.7, 1.5, 0.5],
        [2.2, 1.0],
        [0.8, 1.3, 0.6, 0.9],
    ]
    lit_at = {(1, 1), (3, 0)}
    chrome_at = {(0, 2), (2, 3), (4, 1)}
    gap, depth, h = 0.16, 0.42, 0.28
    for r, row in enumerate(rows):
        x = -2.4 + (0.45 if r % 2 else 0.0)
        y = r * (depth + gap) - 1.0
        for c, length in enumerate(row):
            mat = lit if (r, c) in lit_at else chrome if (r, c) in chrome_at else lac
            lib.rounded_box(f'b{r}{c}', size=(length, depth, h), radius=0.07, loc=(x + length / 2, y, h / 2), mat=mat)
            x += length + gap
    lib.camera(loc=(1.8, -5.6, 3.6), target=(0.2, 0.25, 0.1), lens=50)
    lib.area_light((-3.0, -2.0, 5.0), size=6.0, energy=520, color='#C8C2FF', shape='RECTANGLE')
    lib.area_light((4.0, 3.0, 2.0), size=4.0, energy=480, color='#9C86FF')
    lib.area_light((0.0, -4.0, 1.0), size=5.0, energy=200, color='#6F78FF')


def build_ai(shot):
    bg = '#141A5C'
    lib.world('studio_small_09', strength=0.3, rotation=20, refract=bg, refract_mix=0.8)
    lib.sphere('core', r=0.42, mat=lib.glow('core', inner='#F4F2FF', outer=lib.LILAC, strength=2.0))
    lib.sphere('shell', r=0.72, mat=lib.glass('shell', roughness=0.02, ior=1.35))
    lib.torus('orbit', R=1.45, r=0.012, rot=(math.radians(76), math.radians(-12), 0),
              mat=lib.chrome('orbit', roughness=0.08))
    lib.sphere('sat', r=0.14, loc=(1.32, -0.55, 0.32),
               mat=lib.lacquer('sat', color='#100C2C', metallic=0.35, roughness=0.2))
    lib.camera(loc=(0.0, -6.8, 0.8), target=(0.15, 0, 0.0), lens=48)
    lib.area_light((-3.5, -3.0, 4.0), size=6.0, energy=800, color='#C8C2FF', shape='RECTANGLE')
    lib.area_light((4.0, 2.5, 1.2), size=4.0, energy=900, color='#9C86FF')
    lib.area_light((0.0, -3.0, -4.0), size=6.0, energy=160, color='#6F78FF')


BUILDERS = {'cover-marketing': build_marketing, 'cover-design': build_design,
            'cover-dev': build_dev, 'cover-ai': build_ai}


def build(shot, phase=None):
    lib.reset(samples=shot.get('samples', 224), view='AgX')
    BUILDERS[shot['id']](shot)
