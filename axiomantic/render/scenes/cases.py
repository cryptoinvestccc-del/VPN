"""Case mockups: a laptop and a phone showing the case's site.

The screens are the PNGs made by screens/make.py + shoot.mjs. Devices are
generic (no brand marks), in light aluminium so that they sit on light
case cards; the background is transparent with a soft contact shadow, and
the card behind supplies the case colour."""
import math
import os

import lib

SCREENS = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), '.cache', 'screens')
SLUGS = ['english-school', 'run-app', 'arch-bureau', 'dental-clinic', 'ceramics-shop', 'sport-nutrition']

SHOTS = [{'id': f'case-{slug}', 'slug': slug, 'size': (2000, 1376), 'samples': 128, 'post': {'clean_floor': True},
          'widths': [640, 960, 1280, 2000]} for slug in SLUGS]


def laptop(screen_png):
    alu = lib.chrome('alu', tint='#D9DCE4', roughness=0.32)
    dark = lib.ceramic('deck-dark', color='#2A2C35', roughness=0.5, coat=0.0)
    bezel = lib.ceramic('bezel', color='#0B0C12', roughness=0.15, coat=0.6)
    # base: 3.2 x 2.2, its top at z=0.08
    lib.rounded_box('base', size=(3.2, 2.2, 0.08), radius=0.05, loc=(0, 0, 0.04), mat=alu)
    lib.rounded_box('keys', size=(2.75, 1.0, 0.012), radius=0.02, loc=(0, 0.35, 0.083), mat=dark)
    lib.rounded_box('pad', size=(1.15, 0.68, 0.006), radius=0.04, loc=(0, -0.55, 0.081),
                    mat=lib.chrome('pad', tint='#C9CCD6', roughness=0.25))
    # lid, opened 108 degrees, hinged at the back edge
    hinge_y, hinge_z = 1.08, 0.08
    tilt = math.radians(108 - 90)
    lid = lib.rounded_box('lid', size=(3.2, 0.05, 2.05), radius=0.04,
                          loc=(0, hinge_y + 0.025, hinge_z + 1.025), mat=alu)
    glass = lib.rounded_box('lid-glass', size=(3.16, 0.012, 2.01), radius=0.03,
                            loc=(0, hinge_y - 0.004, hinge_z + 1.025), mat=bezel)
    disp = lib.plane('display', size=1.0, loc=(0, hinge_y - 0.012, hinge_z + 1.06), rot=(math.radians(90), 0, 0),
                     mat=lib.screen('screen-desk', screen_png, strength=1.0))
    disp.scale = (2.96, 1.85, 1)
    for o in (lid, glass, disp):
        # rotate about the hinge line
        o.rotation_mode = 'XYZ'
        pivot = (0, hinge_y, hinge_z)
        y, z = o.location.y - pivot[1], o.location.z - pivot[2]
        o.location.y = pivot[1] + y * math.cos(-tilt) - z * math.sin(-tilt)
        o.location.z = pivot[2] + y * math.sin(-tilt) + z * math.cos(-tilt)
        o.rotation_euler.x += -tilt


def phone(screen_png, loc, rot):
    frame = lib.chrome('phone-frame', tint='#C8CBD4', roughness=0.2)
    body = lib.rounded_box('phone', size=(0.8, 0.07, 1.66), radius=0.13, loc=loc, rot=rot, mat=frame)
    face = lib.rounded_box('phone-glass', size=(0.77, 0.012, 1.63), radius=0.11, loc=loc, rot=rot,
                           mat=lib.ceramic('phone-black', color='#08090E', roughness=0.08, coat=1.0))
    disp = lib.plane('phone-display', size=1.0, loc=loc, rot=(rot[0] + math.radians(90), rot[1], rot[2]),
                     mat=lib.screen('screen-phone', screen_png, strength=1.0))
    disp.scale = (0.72, 1.56, 1)
    # move the glass and the display to the front face of the body
    import mathutils
    front = mathutils.Euler(rot).to_matrix() @ mathutils.Vector((0, -0.041, 0))
    face.location = tuple(mathutils.Vector(loc) + front * 0.85)
    disp.location = tuple(mathutils.Vector(loc) + front * 1.05)
    return body


def build(shot, phase=None):
    slug = shot['slug']
    lib.reset(samples=shot.get('samples', 256), view='Standard')
    # diffuse: the studio's small lamps would throw long hard shadows across the floor
    lib.world('studio_small_03', strength=0.75, rotation=-30, refract=lib.PAGE_LIGHT, refract_mix=0.4,
              diffuse='#FFFFFF', diffuse_strength=0.7)
    laptop(os.path.join(SCREENS, f'{slug}-desktop.png'))
    phone(os.path.join(SCREENS, f'{slug}-phone.png'), loc=(2.05, -0.95, 0.86),
          rot=(math.radians(-8), math.radians(4), math.radians(-14)))
    lib.shadow_catcher(z=0.0)
    lib.camera(loc=(-3.9, -6.6, 3.0), target=(0.35, 0.15, 0.78), lens=50)
    lib.area_light((-3.0, -4.0, 6.0), size=6.0, energy=650)
    lib.area_light((5.0, -3.0, 3.0), size=4.0, energy=420, color=lib.ICE)
    lib.area_light((0.0, 5.0, 4.0), size=5.0, energy=500, color=lib.LILAC)
