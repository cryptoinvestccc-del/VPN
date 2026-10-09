"""«Почему мы»: four small still lifes for the bento grid.

All four sit on light cards (#F4F5FA), so they are rendered with a
transparent background and a soft contact shadow, in the hero's family of
materials: white glazed ceramic, frosted lilac glass, soft chrome,
dark-violet lacquer and a luminous lilac core.

- why-human  a ceramic stylus drawing a glowing lilac stroke on a frosted
             lilac glass tablet (design is drawn by a person)
- why-fast   a lilac core flying out of three trailing rings (speed)
- why-seo    a glass magnifier over a three-bar ranking, the top bar blue
- why-team   four spheres of four materials in one shallow dish
"""
import math

import bmesh
import bpy
import mathutils

import lib

V = mathutils.Vector

SHOTS = [
    {'id': 'why-human', 'size': (1200, 1500), 'samples': 128, 'widths': [480, 800, 1200]},
    {'id': 'why-fast', 'size': (1200, 1200), 'samples': 128, 'widths': [480, 800, 1200]},
    {'id': 'why-seo', 'size': (1200, 1200), 'samples': 128, 'widths': [480, 800, 1200]},
    {'id': 'why-team', 'size': (1200, 1200), 'samples': 128, 'widths': [480, 800, 1200]},
]


# ---------------------------------------------------------------- helpers

def _link(obj):
    bpy.context.scene.collection.objects.link(obj)
    return obj


def rounded(points, radius=0.02, steps=8):
    """Rounds every inner corner of a 2D polyline with a quadratic bezier,
    so that a lathed profile has no hard creases (smooth shading stays clean)."""
    if not isinstance(radius, (list, tuple)) and radius <= 0:
        return list(points)
    pts = [V((p[0], p[1])) for p in points]
    out = [pts[0]]
    for i in range(1, len(pts) - 1):
        a, v, b = pts[i - 1], pts[i], pts[i + 1]
        r = radius[i] if isinstance(radius, (list, tuple)) else radius
        if r <= 0:
            out.append(v)
            continue
        ta = min(r, (v - a).length * 0.5)
        tb = min(r, (b - v).length * 0.5)
        p1 = v + (a - v).normalized() * ta
        p2 = v + (b - v).normalized() * tb
        for k in range(steps + 1):
            t = k / steps
            out.append((1 - t) ** 2 * p1 + 2 * (1 - t) * t * v + t ** 2 * p2)
    out.append(pts[-1])
    return [(p.x, p.y) for p in out]


def lathe(name, profile, segments=128, mat=None):
    """Revolves an (r, z) profile, listed from the bottom pole to the top
    pole, about local Z into a closed smooth solid."""
    me = bpy.data.meshes.new(name)
    verts, faces, rings = [], [], []
    for r, z in profile:
        if r < 1e-6:
            verts.append((0.0, 0.0, z))
            rings.append([len(verts) - 1])
        else:
            ring = []
            for k in range(segments):
                a = 2 * math.pi * k / segments
                verts.append((r * math.cos(a), r * math.sin(a), z))
                ring.append(len(verts) - 1)
            rings.append(ring)
    n = segments
    for A, B in zip(rings, rings[1:]):
        if len(A) == 1 and len(B) == 1:
            continue
        if len(A) == 1:
            faces += [(A[0], B[(k + 1) % n], B[k]) for k in range(n)]
        elif len(B) == 1:
            faces += [(A[k], A[(k + 1) % n], B[0]) for k in range(n)]
        else:
            faces += [(A[k], A[(k + 1) % n], B[(k + 1) % n], B[k]) for k in range(n)]
    me.from_pydata(verts, [], faces)
    bm = bmesh.new()
    bm.from_mesh(me)
    bmesh.ops.remove_doubles(bm, verts=bm.verts, dist=1e-6)
    bmesh.ops.recalc_face_normals(bm, faces=bm.faces)
    bm.to_mesh(me)
    bm.free()
    for p in me.polygons:
        p.use_smooth = True
    o = _link(bpy.data.objects.new(name, me))
    if mat:
        lib.assign(o, mat)
    return o


def orient(obj, loc, direction, roll_up=(0, 0, 1)):
    """Puts a lathed object's local +Z along `direction`, its origin at `loc`."""
    d = V(direction).normalized()
    obj.rotation_mode = 'QUATERNION'
    obj.rotation_quaternion = d.to_track_quat('Z', 'Y')
    obj.location = loc
    return obj


def soft_shadow(mat, color=lib.ICE, amount=0.55):
    """Glass lets light through to the floor instead of casting a black
    shadow: shadow rays see a tinted transparent surface."""
    nt = mat.node_tree
    out = nt.nodes['Material Output']
    src = out.inputs['Surface'].links[0].from_socket
    lp = nt.nodes.new('ShaderNodeLightPath')
    tr = nt.nodes.new('ShaderNodeBsdfTransparent')
    c = lib.lin(color)
    tr.inputs['Color'].default_value = (c[0] * amount, c[1] * amount, c[2] * amount, 1)
    mix = nt.nodes.new('ShaderNodeMixShader')
    nt.links.new(lp.outputs['Is Shadow Ray'], mix.inputs['Fac'])
    nt.links.new(src, mix.inputs[1])
    nt.links.new(tr.outputs['BSDF'], mix.inputs[2])
    nt.links.new(mix.outputs['Shader'], out.inputs['Surface'])
    return mat


def glow_core(name, loc, r, shell=None, strength=2.4, inner='#F6F4FF', outer=lib.LILAC):
    """The hero's luminous dot: a glowing core inside a clear glass shell."""
    core = lib.sphere(name, r=r, loc=loc, mat=lib.glow(name, inner=inner, outer=outer, strength=strength))
    if shell:
        g = soft_shadow(lib.glass(name + '-glass', roughness=0.04, ior=1.33), color=lib.LILAC, amount=0.8)
        lib.sphere(name + '-shell', r=shell, loc=loc, mat=g)
    return core


def light_page(hdri='studio_small_03', strength=0.7, rotation=-30, mix=0.5):
    lib.world(hdri, strength=strength, rotation=rotation, refract=lib.PAGE_LIGHT, refract_mix=mix, diffuse='#FFFFFF', diffuse_strength=0.7)


def rig(key=1100, fill=450, rim=650, rim_color=lib.LILAC, s=1.0, key_loc=None):
    """The house light (lib.light_rig): one soft overhead key casts the only shadow."""
    return lib.light_rig(scale=s, key=key, fill=fill, rim=rim, rim_color=rim_color, target=(0, 0, 0.9))


# ------------------------------------------------------------- why-human

def stylus(nib, direction, length=2.55, r=0.078):
    """A white glazed ceramic stylus: cone tip, chrome ferrule, long body
    with a rounded end. Built along +Z from the nib, then turned."""
    white = lib.ceramic('stylus-white', roughness=0.3, coat=0.6)
    metal = lib.chrome('stylus-chrome', roughness=0.12)
    tip_len, fer_len = 0.36, 0.15
    nib_p = rounded([(0, 0.0), (0.012, 0.0), (0.030, 0.035), (0, 0.035)], 0.01, 6)
    tip_p = rounded([(0, 0.02), (0.022, 0.02), (r * 0.93, tip_len), (0, tip_len)], [0, 0.015, 0.02, 0], 8)
    fer_p = rounded([(0, tip_len - 0.01), (r * 1.0, tip_len - 0.01), (r * 1.0, tip_len + fer_len),
                     (0, tip_len + fer_len)], 0.012, 6)
    top = length
    body_p = [(0, tip_len + fer_len - 0.01), (r * 0.98, tip_len + fer_len - 0.01)]
    body_p += [(r, tip_len + fer_len + 0.02)]
    body_p += [(r, top - r)]
    for k in range(1, 17):
        a = math.radians(90 * k / 16)
        body_p.append((r * math.cos(a), top - r + r * math.sin(a)))
    parts = [lathe('stylus-nib', nib_p, mat=metal), lathe('stylus-tip', tip_p, mat=white),
             lathe('stylus-ferrule', fer_p, mat=metal), lathe('stylus-body', body_p, mat=white)]
    for o in parts:
        orient(o, nib, direction)
    return parts


def stroke(name, points, z, depth=0.03, radii=None, mat=None):
    """A smooth hand-drawn stroke: a bezier through `points` lying at height z,
    as a round tube whose thickness follows `radii` (a pressure curve)."""
    cu = bpy.data.curves.new(name, 'CURVE')
    cu.dimensions = '3D'
    cu.resolution_u = 64
    cu.bevel_depth = depth
    cu.bevel_resolution = 10
    cu.use_fill_caps = True
    sp = cu.splines.new('BEZIER')
    sp.bezier_points.add(len(points) - 1)
    for i, (x, y) in enumerate(points):
        p = sp.bezier_points[i]
        p.co = (x, y, z)
        p.handle_left_type = p.handle_right_type = 'AUTO'
        p.radius = radii[i] if radii else 1.0
    o = _link(bpy.data.objects.new(name, cu))
    bpy.context.view_layer.objects.active = o
    o.select_set(True)
    bpy.ops.object.convert(target='MESH')
    o = bpy.context.object
    lib.smooth(o)
    if mat:
        lib.assign(o, mat)
    return o


def build_human(shot):
    light_page(strength=0.75, rotation=-40, mix=0.55)
    th = 0.12
    tab = lib.frosted('tablet-glass', tint='#D9D4FF', roughness=0.28, ior=1.45)
    soft_shadow(tab, color=lib.LILAC, amount=0.55)
    lib.rounded_box('tablet', size=(2.3, 3.0, th), radius=0.09, loc=(0, 0, th / 2), mat=tab, segments=10)
    ink = lib.glow('stroke', inner='#F3F0FF', outer=lib.VIOLET, strength=3.2)
    pts = [(-0.78, -0.95), (-0.55, -0.25), (-0.05, -0.38), (0.22, 0.2), (0.62, 0.62)]
    stroke('stroke', pts, z=th + 0.012, depth=0.032, radii=[0.35, 0.9, 1.0, 0.95, 0.8], mat=ink)
    nib = V((0.62, 0.62, th + 0.012))
    stylus(nib, (0.42, -0.42, 0.80))
    lib.shadow_catcher(z=0.0)
    lib.camera(loc=(0.6, -6.6, 5.0), target=(0.42, 0.3, 0.8), lens=50)
    rig()


# -------------------------------------------------------------- why-fast

def build_fast(shot):
    light_page(hdri='studio_small_09', strength=0.8, rotation=40, mix=0.5)
    d = V((1.0, -0.35, 0.62)).normalized()
    head = V((0.95, -0.2, 1.95))
    glow_core('lead', head, r=0.22, shell=0.33, strength=2.6)
    mats = [lib.chrome('ring-chrome', roughness=0.1),
            lib.lacquer('ring-lacquer', color='#1B1450', metallic=0.55, roughness=0.22),
            lib.chrome('ring-chrome-2', roughness=0.14)]
    for i, (dist, R, r) in enumerate([(0.75, 0.62, 0.075), (1.55, 0.52, 0.068), (2.3, 0.42, 0.06)]):
        c = head - d * dist
        t = lib.torus(f'ring{i}', R=R, r=r, mat=mats[i])
        t.rotation_mode = 'QUATERNION'
        t.rotation_quaternion = d.to_track_quat('Z', 'Y')
        t.location = c
    lib.shadow_catcher(z=0.0)
    lib.camera(loc=(-0.6, -7.2, 3.6), target=(0.0, 0.0, 1.25), lens=68)
    rig()


# --------------------------------------------------------------- why-seo

def build_seo(shot):
    light_page(strength=0.75, rotation=-30, mix=0.5)
    white = lib.ceramic('bar-white', roughness=0.32, coat=0.5)
    blue = lib.ceramic('bar-blue', color=lib.BLUE, roughness=0.3, coat=0.6)
    w = 0.46
    for i, (x, h, m) in enumerate([(-0.62, 0.62, white), (0.0, 1.0, white), (0.62, 1.48, blue)]):
        lib.rounded_box(f'bar{i}', size=(w, w, h), radius=0.08, loc=(x, 0.2, h / 2), mat=m, segments=8)
    # the magnifier, built facing -Y around its lens centre, then posed
    pivot = bpy.data.objects.new('magnifier', None)
    _link(pivot)
    ring_w = lib.ceramic('mag-white', roughness=0.28, coat=0.7)
    R = 0.62
    ring = lib.torus('mag-ring', R=R, r=0.085, rot=(math.radians(90), 0, 0), mat=ring_w)
    lens_m = soft_shadow(lib.glass('lens', roughness=0.0, ior=1.5), color=lib.ICE, amount=0.75)
    lens = lib.sphere('lens', r=1.0, mat=lens_m, segments=128, rings=64)
    lens.scale = (R - 0.02, 0.1, R - 0.02)
    u = V((math.cos(math.radians(-50)), 0, math.sin(math.radians(-50))))
    hp = rounded([(0, 0), (0.075, 0), (0.088, 1.15), (0, 1.15)], [0, 0.05, 0.08, 0], 10)
    fer = rounded([(0, 0), (0.1, 0), (0.1, 0.16), (0, 0.16)], 0.02, 6)
    handle = lathe('mag-handle', hp, mat=ring_w)
    collar = lathe('mag-collar', fer, mat=lib.chrome('mag-chrome', roughness=0.12))
    orient(collar, u * (R + 0.04), u)
    orient(handle, u * (R + 0.18), u)
    for o in (ring, lens, handle, collar):
        o.parent = pivot
    pivot.location = (0.55, -0.75, 1.55)
    pivot.rotation_euler = (math.radians(12), 0, math.radians(18))
    lib.shadow_catcher(z=0.0)
    lib.camera(loc=(-1.2, -7.0, 3.4), target=(0.2, -0.2, 0.95), lens=72)
    rig()


# -------------------------------------------------------------- why-team

def dish_profile(R=1.7, H=0.3, base=0.06, wall=0.07, foot=1.15):
    outer = [(0, 0), (foot, 0), (R - 0.18, H * 0.55), (R, H)]
    inner = [(R - wall, H), (R - 0.32, H * 0.45 + base * 0.5), (R - 0.75, base), (0, base)]
    return rounded(outer + inner, [0, 0.08, 0.25, 0.03, 0.03, 0.3, 0.35, 0], 12)


def inner_z(profile, rho, rs):
    """Height of the centre of a sphere of radius rs resting in the dish at
    radial distance rho (the dish surface is rotationally symmetric)."""
    best = 0.0
    k = profile.index(max(profile, key=lambda p: p[0]))  # rim: inner surface follows
    inner = profile[k:]
    for (r0, z0), (r1, z1) in zip(inner, inner[1:]):
        for t in (i / 8 for i in range(9)):
            r, z = r0 + (r1 - r0) * t, z0 + (z1 - z0) * t
            for rr in (r, -r):
                dx = rr - rho
                if abs(dx) < rs:
                    best = max(best, z + math.sqrt(rs * rs - dx * dx))
    return best


def build_team(shot):
    light_page(hdri='studio_small_09', strength=0.8, rotation=-20, mix=0.5)
    prof = dish_profile()
    lathe('dish', prof, segments=192, mat=lib.ceramic('dish-white', roughness=0.3, coat=0.7))
    rs = 0.42
    q = rs * 1.005
    spin = math.radians(22)
    spots = [(-q, -q), (q, -q), (-q, q), (q, q)]
    mats = ['lacquer', 'ceramic', 'frosted', 'glow']
    for (x, y), kind in zip(spots, mats):
        x, y = x * math.cos(spin) - y * math.sin(spin), x * math.sin(spin) + y * math.cos(spin)
        z = inner_z(prof, math.hypot(x, y), rs)
        loc = (x, y, z)
        if kind == 'ceramic':
            lib.sphere('s-ceramic', r=rs, loc=loc, mat=lib.ceramic('s-white', roughness=0.3, coat=0.6))
        elif kind == 'frosted':
            m = soft_shadow(lib.frosted('s-frost', tint='#CFC8FF', roughness=0.3), color=lib.LILAC, amount=0.6)
            lib.sphere('s-frost', r=rs, loc=loc, mat=m)
        elif kind == 'lacquer':
            lib.sphere('s-lacquer', r=rs, loc=loc,
                       mat=lib.lacquer('s-lacquer', color='#1B1450', metallic=0.45, roughness=0.22))
        else:
            glow_core('s-glow', loc, r=rs * 0.62, shell=rs, strength=2.6)
    lib.shadow_catcher(z=0.0)
    lib.camera(loc=(0.0, -6.2, 4.3), target=(0.0, 0.0, 0.38), lens=64)
    rig()


BUILDERS = {'why-human': build_human, 'why-fast': build_fast, 'why-seo': build_seo, 'why-team': build_team}


def build(shot, phase=None):
    lib.reset(samples=shot.get('samples', 256), view='AgX')
    BUILDERS[shot['id']](shot)
