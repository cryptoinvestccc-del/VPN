"""One studio for every Axiomantic render.

Every picture on the site is a Cycles render built from these helpers, so
that the hero, the services, the cases and the blog covers share one
palette, one light and one set of materials. Scenes in scenes/ call these
functions; render.py runs them.

Conventions
- Units are metres, +Z is up, the camera looks along +Y unless a scene
  says otherwise.
- Renders have a transparent background (film_transparent). Glass still
  shows what is behind it: rays that pass through glass see `refract`,
  the colour of the page the picture will sit on, so the glass reads as
  glass on that page. Reflections see the studio HDRI.
- Colours are given as sRGB hex and converted to linear here.
"""
import math
import os

import bpy
import mathutils

HERE = os.path.dirname(os.path.abspath(__file__))
HDRI_DIR = os.environ.get('AXM_HDRI_DIR', os.path.join(HERE, '.cache', 'hdri'))
FONT_DIR = os.path.join(HERE, '.cache', 'fonts')

# The brand palette, sRGB.
NAVY = '#070A2A'
NAVY_2 = '#10166A'
BLUE = '#3341FF'
BLUE_LIGHT = '#8E98FF'
VIOLET = '#7B5CFF'
LILAC = '#B9B6FF'
ICE = '#EEF0FF'
MINT = '#8FE3B9'
INK = '#0C0E22'
WHITE = '#FFFFFF'
PAGE_LIGHT = '#F4F5FA'


def lin(hex_color, alpha=1.0):
    """sRGB hex to a linear RGBA tuple."""
    h = hex_color.lstrip('#')
    out = []
    for i in (0, 2, 4):
        c = int(h[i:i + 2], 16) / 255.0
        out.append(c / 12.92 if c <= 0.04045 else ((c + 0.055) / 1.055) ** 2.4)
    return (*out, alpha)


# ------------------------------------------------------------------ scene

def reset(samples=256, look='AgX - Medium High Contrast', view='AgX'):
    """A fresh scene. view='Standard' keeps screen images' colours exact (for
    device mockups); AgX handles bright glass and glow gracefully."""
    bpy.ops.wm.read_factory_settings(use_empty=True)
    s = bpy.context.scene
    s.render.engine = 'CYCLES'
    c = s.cycles
    c.device = 'CPU'
    c.samples = samples
    c.use_adaptive_sampling = True
    c.adaptive_threshold = 0.01
    c.use_denoising = True
    c.denoiser = 'OPENIMAGEDENOISE'
    # Glass needs many transmission bounces or thick parts turn black.
    c.max_bounces = 24
    c.transmission_bounces = 24
    c.glossy_bounces = 8
    c.diffuse_bounces = 4
    c.transparent_max_bounces = 16
    # Caustics are noisy and slow; soft glossy filtering keeps glass clean.
    c.caustics_reflective = False
    c.caustics_refractive = False
    c.blur_glossy = 0.5
    c.sample_clamp_indirect = 8.0
    s.render.film_transparent = True
    c.film_transparent_glass = False
    s.render.image_settings.file_format = 'PNG'
    s.render.image_settings.color_mode = 'RGBA'
    s.render.image_settings.color_depth = '16'
    s.view_settings.view_transform = view
    if view == 'AgX':
        try:
            s.view_settings.look = look
        except TypeError:
            pass
    s.render.threads_mode = 'AUTO'
    return s


def resolution(w, h, percent=100):
    s = bpy.context.scene
    s.render.resolution_x = int(w)
    s.render.resolution_y = int(h)
    s.render.resolution_percentage = percent


def world(hdri='studio_small_09', strength=1.0, rotation=0.0, refract=NAVY, refract_mix=0.65, refract_strength=1.0,
          diffuse=None, diffuse_strength=0.6):
    """Studio HDRI for light and reflections; `refract` for what glass shows.

    refract_mix is how much of the flat page colour replaces the HDRI for
    rays that went through glass (0 = pure studio, 1 = pure page colour).

    diffuse (hex) replaces the HDRI for diffuse rays with an even sky of
    that colour: reflections keep the studio, but the studio's small bright
    lamps stop throwing long hard shadows onto the floor — on light pages
    the only directional shadow then comes from the big soft key.
    """
    s = bpy.context.scene
    w = bpy.data.worlds.new('studio')
    s.world = w
    w.use_nodes = True
    nt = w.node_tree
    nt.nodes.clear()
    out = nt.nodes.new('ShaderNodeOutputWorld')
    coord = nt.nodes.new('ShaderNodeTexCoord')
    mapping = nt.nodes.new('ShaderNodeMapping')
    mapping.inputs['Rotation'].default_value = (0, 0, math.radians(rotation))
    env = nt.nodes.new('ShaderNodeTexEnvironment')
    path = os.path.join(HDRI_DIR, hdri + '.hdr')
    env.image = bpy.data.images.load(path, check_existing=True)
    nt.links.new(coord.outputs['Generated'], mapping.inputs['Vector'])
    nt.links.new(mapping.outputs['Vector'], env.inputs['Vector'])
    bg_hdri = nt.nodes.new('ShaderNodeBackground')
    bg_hdri.inputs['Strength'].default_value = strength
    nt.links.new(env.outputs['Color'], bg_hdri.inputs['Color'])
    bg_flat = nt.nodes.new('ShaderNodeBackground')
    bg_flat.inputs['Color'].default_value = lin(refract)
    bg_flat.inputs['Strength'].default_value = refract_strength
    mix_flat = nt.nodes.new('ShaderNodeMixShader')
    mix_flat.inputs['Fac'].default_value = refract_mix
    nt.links.new(bg_hdri.outputs['Background'], mix_flat.inputs[1])
    nt.links.new(bg_flat.outputs['Background'], mix_flat.inputs[2])
    path_node = nt.nodes.new('ShaderNodeLightPath')
    pick = nt.nodes.new('ShaderNodeMixShader')
    nt.links.new(path_node.outputs['Is Transmission Ray'], pick.inputs['Fac'])
    nt.links.new(bg_hdri.outputs['Background'], pick.inputs[1])
    nt.links.new(mix_flat.outputs['Shader'], pick.inputs[2])
    result = pick.outputs['Shader']
    if diffuse:
        # Only reflections and refractions see the studio; everything else —
        # including the world's direct light on the floor — sees an even sky.
        sky = nt.nodes.new('ShaderNodeBackground')
        sky.inputs['Color'].default_value = lin(diffuse)
        sky.inputs['Strength'].default_value = diffuse_strength
        seen = nt.nodes.new('ShaderNodeMath')
        seen.operation = 'MAXIMUM'
        nt.links.new(path_node.outputs['Is Glossy Ray'], seen.inputs[0])
        nt.links.new(path_node.outputs['Is Transmission Ray'], seen.inputs[1])
        even = nt.nodes.new('ShaderNodeMixShader')
        nt.links.new(seen.outputs['Value'], even.inputs['Fac'])
        nt.links.new(sky.outputs['Background'], even.inputs[1])
        nt.links.new(result, even.inputs[2])
        result = even.outputs['Shader']
        # Importance sampling would aim shadow rays at the studio's lamps, which
        # this shader then answers with the dim even sky: dark speckles on the
        # shadow catcher. An even sky is sampled cleanly by the surfaces alone.
        w.cycles.sampling_method = 'NONE'
    nt.links.new(result, out.inputs['Surface'])
    return w


# -------------------------------------------------------------- materials

def _principled(name):
    m = bpy.data.materials.new(name)
    m.use_nodes = True
    b = m.node_tree.nodes['Principled BSDF']
    return m, b


def glass(name='glass', tint=WHITE, roughness=0.0, ior=1.45, thin_film=0.0, absorb=None):
    """Clear glass. `absorb` (hex) adds a faint body colour through Volume Absorption."""
    m, b = _principled(name)
    b.inputs['Base Color'].default_value = lin(tint)
    b.inputs['Transmission Weight'].default_value = 1.0
    b.inputs['Roughness'].default_value = roughness
    b.inputs['IOR'].default_value = ior
    if thin_film:
        b.inputs['Thin Film Thickness'].default_value = thin_film
        b.inputs['Thin Film IOR'].default_value = 1.33
    if absorb:
        nt = m.node_tree
        vol = nt.nodes.new('ShaderNodeVolumeAbsorption')
        vol.inputs['Color'].default_value = lin(absorb)
        vol.inputs['Density'].default_value = 0.6
        nt.links.new(vol.outputs['Volume'], nt.nodes['Material Output'].inputs['Volume'])
    return m


def frosted(name='frosted', tint=ICE, roughness=0.32, ior=1.45):
    return glass(name, tint=tint, roughness=roughness, ior=ior)


def chrome(name='chrome', tint='#E8EAF5', roughness=0.08):
    m, b = _principled(name)
    b.inputs['Base Color'].default_value = lin(tint)
    b.inputs['Metallic'].default_value = 1.0
    b.inputs['Roughness'].default_value = roughness
    return m


def ceramic(name='ceramic', color=WHITE, roughness=0.38, coat=0.4):
    """Matte-glossy solid, like glazed ceramic or premium plastic."""
    m, b = _principled(name)
    b.inputs['Base Color'].default_value = lin(color)
    b.inputs['Roughness'].default_value = roughness
    b.inputs['Coat Weight'].default_value = coat
    b.inputs['Coat Roughness'].default_value = 0.08
    return m


def soft_plastic(name='plastic', color=BLUE, roughness=0.45):
    m, b = _principled(name)
    b.inputs['Base Color'].default_value = lin(color)
    b.inputs['Roughness'].default_value = roughness
    b.inputs['Subsurface Weight'].default_value = 0.15
    return m


def glow(name='glow', inner=WHITE, outer=BLUE, strength=12.0):
    """A luminous core: white-hot facing the camera, brand blue at the rim."""
    m = bpy.data.materials.new(name)
    m.use_nodes = True
    nt = m.node_tree
    nt.nodes.clear()
    out = nt.nodes.new('ShaderNodeOutputMaterial')
    lw = nt.nodes.new('ShaderNodeLayerWeight')
    lw.inputs['Blend'].default_value = 0.45
    ramp = nt.nodes.new('ShaderNodeValToRGB')
    ramp.color_ramp.elements[0].color = lin(inner)
    ramp.color_ramp.elements[1].color = lin(outer)
    ramp.color_ramp.elements[1].position = 0.85
    mid = ramp.color_ramp.elements.new(0.45)
    mid.color = lin(BLUE_LIGHT)
    em = nt.nodes.new('ShaderNodeEmission')
    em.inputs['Strength'].default_value = strength
    nt.links.new(lw.outputs['Facing'], ramp.inputs['Fac'])
    nt.links.new(ramp.outputs['Color'], em.inputs['Color'])
    nt.links.new(em.outputs['Emission'], out.inputs['Surface'])
    return m


def screen(name, image_path, strength=1.0):
    """A device screen: the image as emission, under a thin glossy layer."""
    m = bpy.data.materials.new(name)
    m.use_nodes = True
    nt = m.node_tree
    nt.nodes.clear()
    out = nt.nodes.new('ShaderNodeOutputMaterial')
    tex = nt.nodes.new('ShaderNodeTexImage')
    tex.image = bpy.data.images.load(image_path, check_existing=True)
    tex.interpolation = 'Cubic'
    em = nt.nodes.new('ShaderNodeEmission')
    em.inputs['Strength'].default_value = strength
    gloss = nt.nodes.new('ShaderNodeBsdfGlossy')
    gloss.inputs['Roughness'].default_value = 0.05
    fres = nt.nodes.new('ShaderNodeFresnel')
    fres.inputs['IOR'].default_value = 1.5
    mix = nt.nodes.new('ShaderNodeMixShader')
    nt.links.new(tex.outputs['Color'], em.inputs['Color'])
    nt.links.new(fres.outputs['Fac'], mix.inputs['Fac'])
    nt.links.new(em.outputs['Emission'], mix.inputs[1])
    nt.links.new(gloss.outputs['BSDF'], mix.inputs[2])
    nt.links.new(mix.outputs['Shader'], out.inputs['Surface'])
    return m


def lacquer(name='lacquer', color='#1B1450', metallic=0.75, roughness=0.2, coat=1.0, coat_roughness=0.04):
    """The brand's dark-violet lacquer: a dark metallic base under a clear coat,
    so soft lights draw long lilac highlights along the curves."""
    m, b = _principled(name)
    b.inputs['Base Color'].default_value = lin(color)
    b.inputs['Metallic'].default_value = metallic
    b.inputs['Roughness'].default_value = roughness
    b.inputs['Coat Weight'].default_value = coat
    b.inputs['Coat Roughness'].default_value = coat_roughness
    return m


def assign(obj, mat):
    obj.data.materials.clear()
    obj.data.materials.append(mat)
    return obj


# --------------------------------------------------------------- geometry

def _link(obj):
    bpy.context.scene.collection.objects.link(obj)
    return obj


def smooth(obj, subdiv=0, autosmooth=True):
    for p in obj.data.polygons:
        p.use_smooth = True
    if subdiv:
        mod = obj.modifiers.new('subd', 'SUBSURF')
        mod.levels = subdiv
        mod.render_levels = subdiv
    return obj


def sphere(name='sphere', r=1.0, loc=(0, 0, 0), mat=None, segments=96, rings=48):
    bpy.ops.mesh.primitive_uv_sphere_add(radius=r, location=loc, segments=segments, ring_count=rings)
    o = bpy.context.object
    o.name = name
    smooth(o)
    if mat:
        assign(o, mat)
    return o


def torus(name='torus', R=1.0, r=0.05, loc=(0, 0, 0), rot=(0, 0, 0), mat=None):
    bpy.ops.mesh.primitive_torus_add(major_radius=R, minor_radius=r, location=loc, rotation=rot,
                                     major_segments=192, minor_segments=32)
    o = bpy.context.object
    o.name = name
    smooth(o)
    if mat:
        assign(o, mat)
    return o


def rounded_box(name='box', size=(1, 1, 1), radius=0.08, loc=(0, 0, 0), rot=(0, 0, 0), mat=None, segments=8):
    bpy.ops.mesh.primitive_cube_add(size=1, location=loc, rotation=rot)
    o = bpy.context.object
    o.name = name
    o.scale = size
    bpy.ops.object.transform_apply(location=False, rotation=False, scale=True)
    bev = o.modifiers.new('bevel', 'BEVEL')
    bev.width = radius
    bev.segments = segments
    bev.limit_method = 'NONE'
    bev.harden_normals = False
    smooth(o)
    if mat:
        assign(o, mat)
    return o


def triangle_ring(name='axiom', R=1.0, tube=0.12, corner=0.28, loc=(0, 0, 0), rot=(0, 0, 0), mat=None):
    """The brand mark in 3D: an equilateral triangle drawn with a round tube,
    corners rounded like the logo stroke. R is the circumradius."""
    curve = bpy.data.curves.new(name + '_curve', 'CURVE')
    curve.dimensions = '3D'
    curve.resolution_u = 48
    curve.bevel_depth = tube
    curve.bevel_resolution = 16
    curve.use_fill_caps = True
    spline = curve.splines.new('BEZIER')
    verts = [mathutils.Vector((R * math.cos(math.radians(90 + 120 * k)), R * math.sin(math.radians(90 + 120 * k)), 0)) for k in range(3)]
    pts = []
    for k in range(3):
        v = verts[k]
        prev_v, next_v = verts[k - 1], verts[(k + 1) % 3]
        a = v + (prev_v - v).normalized() * corner
        b = v + (next_v - v).normalized() * corner
        pts.append((a, v, b))
    spline.bezier_points.add(len(pts) * 2 - 1)
    i = 0
    for a, v, b in pts:
        p = spline.bezier_points[i]
        p.co = a
        p.handle_left_type = p.handle_right_type = 'FREE'
        p.handle_left = a + (a - v) * 0.5
        p.handle_right = a + (v - a) * 0.55
        q = spline.bezier_points[i + 1]
        q.co = b
        q.handle_left_type = q.handle_right_type = 'FREE'
        q.handle_left = b + (v - b) * 0.55
        q.handle_right = b + (b - v) * 0.5
        i += 2
    spline.use_cyclic_u = True
    o = bpy.data.objects.new(name, curve)
    _link(o)
    o.location = loc
    o.rotation_euler = rot
    bpy.context.view_layer.objects.active = o
    o.select_set(True)
    bpy.ops.object.convert(target='MESH')
    o = bpy.context.object
    smooth(o)
    if mat:
        assign(o, mat)
    return o


def letter(name='letter', char='а', font='manrope', weight=700, size=2.0, depth=0.36, voxel=0.025,
           soften=1.0, iterations=40, subdiv=2, loc=(0, 0, 0), rot=(0, 0, 0), mat=None):
    """A glyph of the brand font inflated into soft tubes. The glyph is
    extruded with square edges, remeshed coarsely into one closed surface,
    relaxed with a volume-preserving smooth (edges round off, the letter
    keeps its shape, no bevel spikes at sharp corners) and subdivided at
    render time for a perfectly smooth highlight."""
    curve = bpy.data.curves.new(name + '_text', 'FONT')
    curve.body = char
    curve.font = bpy.data.fonts.load(os.path.join(FONT_DIR, f'{font}-{weight}.ttf'), check_existing=True)
    curve.size = size
    curve.align_x = 'CENTER'
    curve.align_y = 'CENTER'
    curve.extrude = depth / 2
    curve.resolution_u = 24
    o = bpy.data.objects.new(name, curve)
    _link(o)
    bpy.context.view_layer.objects.active = o
    o.select_set(True)
    bpy.ops.object.convert(target='MESH')
    o = bpy.context.object
    rem = o.modifiers.new('remesh', 'REMESH')
    rem.mode = 'VOXEL'
    rem.voxel_size = voxel
    lap = o.modifiers.new('soften', 'LAPLACIANSMOOTH')
    lap.lambda_factor = soften
    lap.iterations = iterations
    lap.use_volume_preserve = True
    bpy.ops.object.modifier_apply(modifier='remesh')
    bpy.ops.object.modifier_apply(modifier='soften')
    smooth(o, subdiv=subdiv)
    o.location = loc
    o.rotation_euler = rot
    if mat:
        assign(o, mat)
    return o


def plane(name='plane', size=10, loc=(0, 0, 0), rot=(0, 0, 0), mat=None):
    bpy.ops.mesh.primitive_plane_add(size=size, location=loc, rotation=rot)
    o = bpy.context.object
    o.name = name
    if mat:
        assign(o, mat)
    return o


def shadow_catcher(z=0.0, size=40):
    """An invisible floor that keeps only the shadows (alpha in the PNG)."""
    p = plane('shadow_catcher', size=size, loc=(0, 0, z))
    p.is_shadow_catcher = True
    return p


# ------------------------------------------------------------ camera, light

def look_at(obj, target):
    direction = mathutils.Vector(target) - obj.location
    obj.rotation_euler = direction.to_track_quat('-Z', 'Y').to_euler()


def camera(loc=(0, -6, 2), target=(0, 0, 0), lens=70, dof=None, fstop=4.0):
    cam_data = bpy.data.cameras.new('cam')
    cam_data.lens = lens
    cam_data.sensor_width = 36
    if dof:
        cam_data.dof.use_dof = True
        cam_data.dof.focus_distance = dof
        cam_data.dof.aperture_fstop = fstop
    cam = bpy.data.objects.new('cam', cam_data)
    _link(cam)
    cam.location = loc
    look_at(cam, target)
    bpy.context.scene.camera = cam
    return cam


def area_light(loc, target=(0, 0, 0), size=3.0, energy=500, color=WHITE, shape='DISK', size_y=None):
    data = bpy.data.lights.new('area', 'AREA')
    data.energy = energy
    data.size = size
    data.shape = shape
    # A rectangle left at Blender's default height (1 m) is a strip light, and
    # a strip throws shadows stretched along it; rectangles are square unless asked.
    data.size_y = size if size_y is None else size_y
    data.color = lin(color)[:3]
    o = bpy.data.objects.new('area', data)
    _link(o)
    o.location = loc
    look_at(o, target)
    return o


def studio_rig(key=900, fill=250, rim=700, rim_color=VIOLET, scale=1.0):
    """The house three-point light: soft key top-left, fill right, coloured rim behind."""
    area_light((-4 * scale, -4 * scale, 5 * scale), size=4 * scale, energy=key)
    area_light((5 * scale, -3 * scale, 1.5 * scale), size=3 * scale, energy=fill, color=ICE)
    area_light((1 * scale, 5 * scale, 3 * scale), size=3 * scale, energy=rim, color=rim_color)


def light_rig(scale=1.0, key=1100, fill=450, rim=650, rim_color=LILAC, target=(0, 0, 0.8)):
    """The house light for light pages: one big softbox almost overhead (the
    only light that casts a shadow — a short, soft one under the object), a
    large shadowless fill from the camera side and a lilac rim behind."""
    s = scale
    k = area_light((-1.2 * s, -1.8 * s, 6.5 * s), target=target, size=4.0 * s, energy=key, shape='SQUARE')
    f = area_light((3.5 * s, -6.0 * s, 2.5 * s), target=target, size=6.0 * s, energy=fill, color=ICE)
    r = area_light((1.0 * s, 5.5 * s, 3.5 * s), target=target, size=5.0 * s, energy=rim, color=rim_color)
    for light in (f, r):
        light.data.use_shadow = False
    return k, f, r


# ----------------------------------------------------------------- render

def render(path, w=None, h=None, samples=None):
    s = bpy.context.scene
    if w and h:
        resolution(w, h)
    if samples:
        s.cycles.samples = samples
    s.render.filepath = path
    os.makedirs(os.path.dirname(path), exist_ok=True)
    bpy.ops.render.render(write_still=True)
    return path
