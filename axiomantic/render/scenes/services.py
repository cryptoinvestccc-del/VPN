"""Services and approach: one object per idea, in the hero's materials.

svc-landing   one page, one goal: a frosted lilac glass slab with a single
              glowing bead sunk into it at call-to-action height
svc-multi     a multi-page site: three page panels receding in depth,
              linked by one chrome sphere
svc-custom    a custom product: a dark lacquer core wired to three
              satellites (chrome, ceramic, light) by thin chrome tubes
approach      people + AI: a luminous core in a glass ring, three spheres
              (marketer, designer, developer) on a chrome orbit around it

The three service shots are light: transparent background, a shadow
catcher floor, glass refracting the page colour. The approach shot is a
dark scene composited on the section's navy.
"""
import math

import bmesh
import bpy
import mathutils

import lib

SHOTS = [
    {'id': 'svc-landing', 'size': (1600, 1200), 'samples': 128, 'widths': [480, 800, 1200, 1600]},
    {'id': 'svc-multi', 'size': (1600, 1200), 'samples': 128, 'widths': [480, 800, 1200, 1600]},
    {'id': 'svc-custom', 'size': (1600, 1200), 'samples': 128, 'widths': [480, 800, 1200, 1600]},
    {'id': 'approach', 'size': (1400, 1400), 'samples': 128,
     'post': {'bg': '#0B0E33', 'bloom': 0.5}, 'widths': [480, 800, 1200, 1400]},
]


# ---------------------------------------------------------------- helpers

def outline(w, h, r, n=24):
    """A rounded rectangle, counter-clockwise, as (x, z) points."""
    pts = []
    corners = [(w / 2 - r, h / 2 - r, 0), (-w / 2 + r, h / 2 - r, 90),
               (-w / 2 + r, -h / 2 + r, 180), (w / 2 - r, -h / 2 + r, 270)]
    for cx, cz, a0 in corners:
        for i in range(n + 1):
            a = math.radians(a0 + 90 * i / n)
            pts.append((cx + r * math.cos(a), cz + r * math.sin(a)))
    return pts


def slab(name, w, h, t, corner, edge, mat=None, hole=None, loc=(0, 0, 0), rot=(0, 0, 0)):
    """A rounded-rectangle plate standing in the XZ plane, `t` thick along Y.

    `corner` rounds the outline, `edge` rounds the faces into the sides;
    harden_normals keeps the big faces optically flat (no smooth-shading
    wobble in glass). `hole` = (x, z, r) cuts a round notch through it."""
    me = bpy.data.meshes.new(name)
    bm = bmesh.new()
    pts = outline(w, h, corner)
    front = [bm.verts.new((x, -t / 2, z)) for x, z in pts]
    back = [bm.verts.new((x, t / 2, z)) for x, z in pts]
    bm.faces.new(front)
    bm.faces.new(list(reversed(back)))
    n = len(pts)
    for i in range(n):
        j = (i + 1) % n
        bm.faces.new((front[i], back[i], back[j], front[j]))
    bmesh.ops.recalc_face_normals(bm, faces=bm.faces)
    bm.to_mesh(me)
    bm.free()
    o = bpy.data.objects.new(name, me)
    bpy.context.scene.collection.objects.link(o)
    bpy.context.view_layer.objects.active = o
    o.select_set(True)
    if hole:
        hx, hz, hr = hole
        bpy.ops.mesh.primitive_cylinder_add(vertices=96, radius=hr, depth=t * 4, location=(hx, 0, hz),
                                            rotation=(math.radians(90), 0, 0))
        cutter = bpy.context.object
        bpy.context.view_layer.objects.active = o
        mod = o.modifiers.new('notch', 'BOOLEAN')
        mod.operation = 'DIFFERENCE'
        mod.solver = 'EXACT'
        mod.object = cutter
        bpy.ops.object.modifier_apply(modifier='notch')
        bpy.data.objects.remove(cutter, do_unlink=True)
    bev = o.modifiers.new('bevel', 'BEVEL')
    bev.width = edge
    bev.segments = 10
    bev.limit_method = 'ANGLE'
    bev.angle_limit = math.radians(40)
    bev.harden_normals = True
    # The big faces stay flat-shaded: after the notch they are fans of long
    # triangles, and interpolated normals would draw their edges in glass.
    for p in o.data.polygons:
        p.use_smooth = abs(p.normal.y) < 0.99
    o.location = loc
    o.rotation_euler = rot
    if mat:
        lib.assign(o, mat)
    return o


def tube(name, pts, radius, mat=None, closed=False, smooth_path=True):
    """A round tube along points (Bezier with auto handles = a smooth path)."""
    cu = bpy.data.curves.new(name, 'CURVE')
    cu.dimensions = '3D'
    cu.resolution_u = 32
    cu.bevel_depth = radius
    cu.bevel_resolution = 10
    cu.use_fill_caps = True
    if smooth_path:
        sp = cu.splines.new('BEZIER')
        sp.bezier_points.add(len(pts) - 1)
        for bp, p in zip(sp.bezier_points, pts):
            bp.co = p
            bp.handle_left_type = bp.handle_right_type = 'AUTO'
    else:
        sp = cu.splines.new('POLY')
        sp.points.add(len(pts) - 1)
        for pp, p in zip(sp.points, pts):
            pp.co = (*p, 1.0)
    sp.use_cyclic_u = closed
    o = bpy.data.objects.new(name, cu)
    bpy.context.scene.collection.objects.link(o)
    bpy.context.view_layer.objects.active = o
    for other in bpy.context.selected_objects:
        other.select_set(False)
    o.select_set(True)
    bpy.ops.object.convert(target='MESH')
    o = bpy.context.object
    lib.smooth(o)
    if mat:
        lib.assign(o, mat)
    return o


def place(obj_or_vec, loc=(0, 0, 0), rot=(0, 0, 0)):
    """World position of a local point of an object posed at loc/rot."""
    return mathutils.Vector(loc) + mathutils.Euler(rot).to_matrix() @ mathutils.Vector(obj_or_vec)


def floor(z=0.0, color=lib.PAGE_LIGHT):
    """Shadow catcher that, seen through glass or in reflections, is the
    page itself (instead of Cycles' default grey)."""
    p = lib.shadow_catcher(z=z)
    m = bpy.data.materials.new('floor')
    m.use_nodes = True
    nt = m.node_tree
    nt.nodes.clear()
    out = nt.nodes.new('ShaderNodeOutputMaterial')
    dif = nt.nodes.new('ShaderNodeBsdfDiffuse')
    dif.inputs['Color'].default_value = lib.lin(color)
    em = nt.nodes.new('ShaderNodeEmission')
    em.inputs['Color'].default_value = lib.lin(color)
    em.inputs['Strength'].default_value = 1.0
    lp = nt.nodes.new('ShaderNodeLightPath')
    mix = nt.nodes.new('ShaderNodeMixShader')
    nt.links.new(lp.outputs['Is Transmission Ray'], mix.inputs['Fac'])
    nt.links.new(dif.outputs['BSDF'], mix.inputs[1])
    nt.links.new(em.outputs['Emission'], mix.inputs[2])
    nt.links.new(mix.outputs['Shader'], out.inputs['Surface'])
    lib.assign(p, m)
    return p


def lilac_glass(name, tint='#D3CEFF', roughness=0.16, absorb='#A99CFF', density=3.0, ior=1.45):
    """Frosted lilac glass with body colour: clear on the faces, deeper lilac
    where the light travels further (edges, the notch)."""
    m = lib.glass(name, tint=tint, roughness=roughness, ior=ior, absorb=absorb)
    for n in m.node_tree.nodes:
        if n.bl_idname == 'ShaderNodeVolumeAbsorption':
            n.inputs['Density'].default_value = density
    return m


def light_studio(card=lib.PAGE_LIGHT, hdri='studio_small_03', strength=0.8, rotation=-30):
    lib.world(hdri, strength=strength, rotation=rotation, refract=card, refract_mix=0.55, diffuse='#FFFFFF', diffuse_strength=0.7)


def soft_rig():
    """Big soft key high on the left, a cool fill on the right that lifts the
    shadow side, a lilac rim behind."""
    lib.light_rig(target=(0, 0, 1.0))


# ------------------------------------------------------------------ shots

def build_landing(shot):
    lib.reset(samples=shot.get('samples', 256), view='AgX')
    light_studio()
    W, H, T = 1.06, 2.04, 0.12
    yaw = math.radians(-32)
    rot = (0, 0, yaw)
    cta = (0.0, -0.36)  # x, z on the slab (centre = 0), the call-to-action height
    base_z = H / 2
    glass = lilac_glass('slab-glass')
    # The bead sits half sunk into the face. A boolean notch would leave its
    # triangulation visible in the glass as fine lines.
    slab('slab', W, H, T, corner=0.17, edge=0.05, mat=glass, loc=(0, 0, base_z), rot=rot)
    rim = lib.chrome('rim', tint='#E9EAF4', roughness=0.12)
    e = 0.004
    pts = [(x, 0.0, z) for x, z in outline(W + 2 * e, H + 2 * e, 0.17 + e, n=24)]
    band = tube('rim', pts, 0.018, mat=rim, closed=True, smooth_path=False)
    band.location = (0, 0, base_z)
    band.rotation_euler = rot
    bead_loc = place((cta[0], -T * 0.5, cta[1]), (0, 0, base_z), rot)
    lib.sphere('bead', r=0.118, loc=tuple(bead_loc),
               mat=lib.glow('bead', inner='#E2DDFF', outer=lib.VIOLET, strength=2.6))
    # a thin chrome collar where the bead meets the glass: the "button"
    lib.torus('collar', R=0.128, r=0.011, loc=tuple(bead_loc), rot=(math.radians(90), 0, yaw), mat=rim)
    floor()
    lib.camera(loc=(1.35, -6.4, 1.75), target=(0.05, 0, 1.0), lens=66)
    soft_rig()


def build_multi(shot):
    lib.reset(samples=shot.get('samples', 256), view='AgX')
    light_studio()
    ceramic = lib.ceramic('panel-ceramic', color='#FBFBFE', roughness=0.3, coat=0.6)
    glass1 = lib.frosted('panel-glass-1', tint='#D8D3FF', roughness=0.22)
    glass2 = lib.frosted('panel-glass-2', tint='#C9C3FF', roughness=0.26)
    yaw = math.radians(-28)
    # front to back: size shrinks, each sits further back and to the right
    panels = [
        ('p-front', 1.30, 1.72, 0.10, ceramic, (-0.55, -0.55)),
        ('p-mid', 1.10, 1.46, 0.08, glass1, (0.05, 0.25)),
        ('p-back', 0.92, 1.22, 0.08, glass2, (0.62, 1.05)),
    ]
    for i, (name, w, h, t, mat, (x, y)) in enumerate(panels):
        slab(name, w, h, t, corner=0.11, edge=0.035, mat=mat, loc=(x, y, h / 2),
             rot=(0, 0, yaw + math.radians(4 * i)))
    lib.sphere('link', r=0.16, loc=(-1.12, -0.95, 1.66), mat=lib.chrome('link', roughness=0.06))
    floor()
    lib.camera(loc=(-1.2, -6.8, 2.1), target=(0.08, 0.1, 0.82), lens=60)
    lib.light_rig(target=(0, 0, 0.9))


def build_custom(shot):
    lib.reset(samples=shot.get('samples', 256), view='AgX')
    light_studio()
    core_rot = (0, 0, math.radians(28))
    S = 1.0
    lib.rounded_box('core', size=(S, S, S), radius=0.2, loc=(0, 0, S / 2), rot=core_rot,
                    mat=lib.lacquer('core', color='#1A1446', metallic=0.4, roughness=0.22, coat_roughness=0.04),
                    segments=12)
    chrome = lib.chrome('wire', tint='#E6E8F3', roughness=0.1)
    sats = [
        # sphere centre, radius, material, the face it leaves from (local)
        ((-1.55, -0.15, 1.15), 0.2, lib.chrome('sat-chrome', roughness=0.05), (-0.5, 0.0, 0.62)),
        ((1.5, -0.45, 0.62), 0.22, lib.ceramic('sat-ceramic', color='#FFFFFF', roughness=0.3, coat=0.6), (0.5, 0.0, 0.42)),
        ((0.55, 0.55, 1.95), 0.16, lib.glow('sat-glow', inner='#F6F4FF', outer=lib.VIOLET, strength=4.0), (0.0, 0.0, 1.0)),
    ]
    for i, (c, r, mat, face) in enumerate(sats):
        lib.sphere(f'sat{i}', r=r, loc=c, mat=mat)
        start = place((face[0] * 0.92, face[1], face[2] - S / 2), (0, 0, S / 2), core_rot)
        c = mathutils.Vector(c)
        normal = (start - mathutils.Vector((0, 0, S / 2)))
        normal.z = normal.z if face[2] >= 1.0 else 0
        normal.normalize()
        mid = start + normal * 0.45
        tube(f'wire{i}', [start - normal * 0.05, mid, c.lerp(mid, 0.35), c], 0.018, mat=chrome)
    floor()
    lib.camera(loc=(0.6, -6.6, 2.4), target=(0.0, 0, 0.98), lens=58)
    lib.light_rig(target=(0, 0, 0.9))


def build_approach(shot):
    lib.reset(samples=shot.get('samples', 256), view='AgX')
    lib.world('studio_small_09', strength=0.22, rotation=25, refract='#0B0E33', refract_mix=0.8)
    lib.sphere('core', r=0.34, mat=lib.glow('core', inner='#F2F0FF', outer=lib.LILAC, strength=2.3))
    ring_rot = (math.radians(68), math.radians(-16), math.radians(12))
    lib.torus('ring', R=0.66, r=0.105, rot=ring_rot, mat=lib.glass('ring-glass', roughness=0.03, ior=1.4))
    orbit_rot = (math.radians(74), math.radians(9), 0)
    R = 1.5
    lib.torus('orbit', R=R, r=0.0075, rot=orbit_rot, mat=lib.chrome('orbit', roughness=0.08))
    mats = [
        lib.lacquer('marketer', color='#1B1450', metallic=0.4, roughness=0.2),
        lib.ceramic('designer', color='#F4F3FF', roughness=0.3, coat=0.6),
        lib.lacquer('developer', color='#5A43E0', metallic=0.2, roughness=0.25),
    ]
    for i, (ang, mat) in enumerate(zip((-58, 62, 182), mats)):
        a = math.radians(ang)
        p = mathutils.Euler(orbit_rot).to_matrix() @ mathutils.Vector((R * math.cos(a), R * math.sin(a), 0))
        lib.sphere(f'role{i}', r=0.15, loc=tuple(p), mat=mat)
    lib.camera(loc=(0.0, -6.2, 0.9), target=(0, 0, -0.02), lens=58)
    lib.area_light((-3.5, -3.0, 4.0), size=6.0, energy=700, color='#C8C2FF', shape='RECTANGLE')
    lib.area_light((4.0, 2.5, 1.2), size=4.0, energy=900, color='#9C86FF')
    lib.area_light((-4.0, 3.0, 0.5), size=4.0, energy=900, color='#7B5CFF')
    lib.area_light((0.0, -3.0, -4.0), size=6.0, energy=120, color='#6F78FF')


BUILDERS = {
    'svc-landing': build_landing,
    'svc-multi': build_multi,
    'svc-custom': build_custom,
    'approach': build_approach,
}


def build(shot, phase=None):
    BUILDERS[shot['id']](shot)
