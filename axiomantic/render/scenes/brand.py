"""The brand object: the logo's lowercase «а» in dark-violet lacquer and
its dot as a small luminous sphere. In the loop the letter sways and the
studio light slides along its curves; both motions are sine waves over
the loop, so the last frame meets the first."""
import math

import lib

SHOTS = [
    # the poster is the loop's first frame at full quality
    {'id': 'hero', 'size': (1600, 1600), 'samples': 512,
     'post': {'bg': '#0E1240', 'bloom': 0.45}, 'widths': [560, 800, 1120, 1600]},
    # 6 s at 24 fps; 960 px is enough because the video sits in a ~640 px box
    {'id': 'hero-loop', 'size': (960, 960), 'samples': 96, 'frames': 144, 'fps': 24,
     'post': {'bg': '#0E1240', 'bloom': 0.45}, 'video_sizes': [960, 640]},
]


def pose(objs, phase):
    """Moves an already built scene to a point of the loop (0..1)."""
    w = 2 * math.pi * phase
    a, dot, shell, mapping = objs
    yaw = math.radians(-34 + 10 * math.sin(w))
    pitch = math.radians(12 + 3 * math.sin(w + math.pi / 2))
    a.rotation_euler = (math.radians(90) + pitch, math.radians(-7), yaw)
    a.location = (0, 0, 0.04 * math.sin(w))
    z = -1.0 + 0.05 * math.sin(w + 1.2)
    dot.location = shell.location = (1.08, -0.9, z)
    mapping.inputs['Rotation'].default_value = (0, 0, math.radians(25 + 18 * math.sin(w)))


def build(shot, phase=None):
    lib.reset(samples=shot.get('samples', 256))
    world = lib.world('studio_small_09', strength=0.25, rotation=25, refract='#0B0E33', refract_mix=0.8)
    mapping = next(n for n in world.node_tree.nodes if n.bl_idname == 'ShaderNodeMapping')
    a = lib.letter('a', char='а', size=3.6, depth=0.44,
                   mat=lib.lacquer('a-lacquer', color='#100C2C', metallic=0.35, roughness=0.22, coat_roughness=0.035))
    dot = lib.sphere('dot', r=0.1, mat=lib.glow('dot', inner='#F4F2FF', outer=lib.LILAC, strength=2.2))
    shell = lib.sphere('dot-shell', r=0.16, mat=lib.glass('dot-glass', roughness=0.05, ior=1.33))

    lib.camera(loc=(0.4, -5.2, 0.5), target=(0.12, 0, -0.12), lens=58)
    # long soft lilac highlights from above-left, a cool rim from the right,
    # a faint blue bounce from below — the reference's lighting
    lib.area_light((-3.5, -3.0, 4.0), size=7.0, energy=1500, color='#C8C2FF', shape='RECTANGLE', size_y=1.0)
    lib.area_light((4.5, 0.5, 1.5), size=5.0, energy=850, color='#C9C4FF', shape='RECTANGLE', size_y=1.0)
    lib.area_light((-4.0, 3.0, 0.5), size=4.0, energy=900, color='#9C86FF')
    lib.area_light((0.0, -3.0, -4.0), size=6.0, energy=140, color='#6F78FF')
    objs = (a, dot, shell, mapping)
    pose(objs, phase or 0.0)
    return objs
