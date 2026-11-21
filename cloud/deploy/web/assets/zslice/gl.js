// gl.js: draws the frame data of the 3D view (view.js) with WebGL2.
//
// The draw calls are those of the Vulkan backend (src/render/vk.zig,
// Renderer.draw): the grid lines, the beads (instanced draws of one bead
// mesh), the marks, the beads out of a focus as ghosts (blended, no depth
// write), the extras, and the see-through toolhead of a replay. The
// shaders are ports of src/render/shaders/*.vert and *.frag to GLSL ES
// 3.00; change them together.
//
// WebGL2 has no base instance, so a draw that starts at instance k binds
// the instance attributes k * 32 bytes into the buffer, and the shader adds
// k (the uniform `base`) to gl_InstanceID.
//
// The other plates of a set (src/render/plates.zig) have a bead buffer
// each (a slot), uploaded when its generation changes. They draw with the
// same program, with their own matrix, and `fade` takes the colors of a
// plate with an old slice toward the background.
//
// SPDX-License-Identifier: AGPL-3.0-or-later

const MAX_EXTRAS = 4096; // max_extras in src/render/frame.zig
const STRIDE = 32; // the size of one instance (zs_web_instance)
// The depth pass costs a second run of the vertex shader. It pays off
// with many full beads on the screen, where the hidden pixels are most
// of the work. GPU times of the same frames on an Intel HD 530 (1400 x
// 900, near beads first, culling on): 83 000 full beads 121 ms with no
// depth pass, 62 ms with it; 66 000: 46 and 50 ms; 48 000 and fewer:
// the same.
const DEPTH_PASS_BEADS = 70000;

const boxVert = `#version 300 es
// A port of src/render/shaders/box.vert. One bead of plastic for each
// toolpath segment (instanced).
precision highp float;
precision highp int;

// The depth pass and the color pass must give the same depth to the bit.
invariant gl_Position;

layout(location = 0) in vec4 tv;
layout(location = 2) in vec4 seg;
layout(location = 3) in vec3 zhw;
layout(location = 4) in vec4 color;

uniform mat4 mvp;
uniform vec4 light;
uniform vec4 eye;
uniform vec4 params;
uniform float base;

out vec3 vcolor;
out vec3 vnormal;
out vec3 vpos;
out float vhigh;
out float vfocus;
out vec4 vsec;
out vec3 vend;
out float vh;

const vec3 feature_rgb[14] = vec3[14](
    vec3(0.851, 0.282, 0.059), vec3(0.961, 0.624, 0.000),
    vec3(0.110, 0.494, 0.839), vec3(0.169, 0.541, 0.243),
    vec3(0.682, 0.243, 0.788), vec3(0.553, 0.431, 0.388),
    vec3(0.678, 0.710, 0.741), vec3(0.286, 0.314, 0.341),
    vec3(0.047, 0.651, 0.682), vec3(0.510, 0.788, 0.118),
    vec3(0.400, 0.800, 0.949),
    vec3(0.878, 0.102, 0.149), vec3(0.200, 0.451, 0.949),
    vec3(0.949, 0.251, 0.749));

void main() {
    vec2 a = seg.xy;
    vec2 b = seg.zw;
    vec2 d = b - a;
    float len = length(d);
    vec2 dir = len > 0.0 ? d / len : vec2(1.0, 0.0);
    vec2 side = vec2(-dir.y, dir.x);
    float h = zhw.y;
    float w = zhw.z;
    vec2 c = mix(a, b, tv.x);
    float r = min(h, w) * 0.5;
    float core = max(0.5 * w - r, 0.0);
    float zmid = zhw.x - 0.5 * h;
    vec3 pos;
    vec3 n;
    vend = vec3(0.0);
    if (tv.w > 1.5) {
        vec3 d3 = vec3(dir, 0.0);
        vec3 s3 = vec3(side, 0.0);
        vec3 e = eye.xyz - vec3(0.5 * (a + b), zmid);
        e = length(e) > 0.0 ? normalize(e) : vec3(0.0, 0.0, 1.0);
        vec3 al = d3 - e * dot(d3, e);
        al = length(al) > 1e-4 ? normalize(al) : normalize(cross(e, abs(e.z) < 0.9 ? vec3(0.0, 0.0, 1.0) : vec3(1.0, 0.0, 0.0)));
        vec3 ac = cross(e, al);
        float flat_w = core * abs(dot(ac, s3));
        float hw = flat_w + r;
        float ext = max(length(vec2(0.5 * w * length(al.xy), r * al.z)), 1e-4);
        vec3 end = vec3(tv.x < 0.5 ? a : b, zmid);
        pos = end + al * ((tv.x < 0.5 ? -1.0 : 1.0) * ext) + ac * (tv.y * hw) + e * r;
        n = ac;
        vsec = vec4(dot(e, s3) < 0.0 ? -side : side, 0.5 * tv.y, max(r, 1e-4));
        float q = (len * dot(d3, al) + ext) / ext;
        vend = vec3(tv.x < 0.5 ? vec2(-1.0, q) : vec2(q, -1.0), flat_w);
    } else if (tv.w > 0.5) {
        pos = vec3(c, zmid + r);
        n = vec3(0.0, 0.0, 1.0);
        vsec = vec4(0.0, 0.0, 0.5, 0.0);
    } else if (tv.w < -0.5) {
        pos = vec3(c, zmid - r);
        n = vec3(0.0, 0.0, -1.0);
        vsec = vec4(0.0, 0.0, -0.5, 0.0);
    } else {
        vec2 np = dir * cos(tv.y) + side * sin(tv.y);
        vsec = vec4(np, tv.z / 3.14159265, 0.0);
        uint feat = uint(color.a * 255.0 + 0.5);
        if (feat == 2u || feat == 7u || feat == 4u || feat == 9u) {
            float hw = 0.52 * w;
            float hh = 0.5 * h;
            float rr = 0.15 * min(h, w);
            float sp = sin(tv.z);
            float cp = cos(tv.z);
            float x = (hw - rr) + rr * cp;
            float z = sign(sp) * (hh - rr) + rr * sp;
            pos = vec3(c + np * x, zmid + z);
            n = normalize(vec3(np * cp, sp));
        } else {
            float out_r = core + r * cos(tv.z);
            pos = vec3(c + np * out_r, zmid + r * sin(tv.z));
            n = normalize(vec3(np * cos(tv.z), sin(tv.z)));
        }
    }
    vh = h;
    vcolor = color.rgb;
    vnormal = n;
    vpos = pos;
    float idx = float(gl_InstanceID) + base;
    vhigh = (idx >= params.x && idx < params.y) ? 1.0 : 0.0;
    bool isolate = params.z < -0.5;
    uint mask = isolate ? 0u : uint(params.z + 0.5);
    uint feature = uint(color.a * 255.0 + 0.5);
    bool in_mask = ((mask >> feature) & 1u) != 0u;
    vfocus = (mask == 0u || (in_mask && zhw.x <= eye.w + 1e-4)) ? 1.0 : 0.0;
    if (isolate) vfocus = vhigh;
    if (mask != 0u && vfocus > 0.5 && params.w < 0.5) vcolor = feature_rgb[min(feature, 13u)];
    if (feature >= 14u) {
        vfocus = 1.0;
        vhigh = -1.0;
    } else if (feature >= 12u && !in_mask) {
        gl_Position = vec4(0.0, 0.0, 0.0, 0.0);
        return;
    }
    gl_Position = mvp * vec4(pos, 1.0);
    // The matrix is for the clip space of Vulkan: y down, depth 0 to 1.
    gl_Position.y = -gl_Position.y;
    gl_Position.z = 2.0 * gl_Position.z - gl_Position.w;
}`;

const boxFrag = `#version 300 es
// A port of src/render/shaders/box.frag.
precision highp float;

uniform vec4 light;
uniform vec4 eye;
uniform vec4 params;
uniform float fade;

in vec3 vcolor;
in vec3 vnormal;
in vec3 vpos;
in float vhigh;
in float vfocus;
in vec4 vsec;
in vec3 vend;
in float vh;
out vec4 frag;

vec3 toLinear(vec3 c) { return pow(c, vec3(2.2)); }
vec3 toSrgb(vec3 c) { return pow(clamp(c, 0.0, 1.0), vec3(1.0 / 2.2)); }

// The light of a point of a bead with normal n (linear color).
vec3 shade(vec3 n, vec3 l, vec3 v, vec3 base, float gloss) {
    float ambient = mix(0.18, 0.32, 0.5 + 0.5 * n.z);
    float diffuse = max(dot(n, l), 0.0);
    vec3 hv = normalize(l + v);
    float spec = pow(max(dot(n, hv), 0.0), 40.0) * 0.18 * gloss;
    return base * (ambient + 0.85 * diffuse) + vec3(spec);
}

void main() {
    vec3 v = normalize(eye.xyz - vpos);
    vec3 n = normalize(vnormal);
    vec3 sd = vec3(vsec.xy, 0.0);
    vec3 ev = normalize(sd * dot(v, sd) + vec3(0.0, 0.0, v.z + 1e-4));
    if (vsec.w > 0.0) {
        float t = 2.0 * vsec.z;
        float o = min(min(vend.x, vend.y), 0.0);
#ifndef SOLID
        if (o * o + t * t > 1.0) discard;
#endif
        // The bead seen from the eye: a round side, the flat top (or
        // bottom) face, and the other round side. u: the place on a
        // round side, from its middle (-1 to 1).
        float r = vsec.w;
        float f = vend.z;
        vec3 ac = normalize(vnormal);
        vec3 zf = vec3(0.0, 0.0, ev.z < 0.0 ? -1.0 : 1.0);
        float zac = dot(zf, ac);
        float x = t * (f + r);
        float u = 0.0;
        n = zf;
        if (x > f + r * zac) u = clamp((x - f) / r, -1.0, 1.0);
        else if (x < r * zac - f) u = clamp((x + f) / r, -1.0, 1.0);
        if (u != 0.0) n = normalize(ev * sqrt(max(1.0 - u * u, 0.0)) + ac * u);
    }
    float px = 1.0 / max(fwidth(vsec.z), 1e-6);
    float flat_k = 1.0 - smoothstep(2.0, 5.0, px);
    bool focus = params.z > 0.5 || params.z < -0.5;
    if (focus && params.w > 1.5 && params.w < 2.5) {
#ifndef SOLID
        if (vfocus > 0.5) discard;
#endif
        float rim = 1.0 - abs(dot(n, v));
        frag = vec4(vec3(0.55, 0.58, 0.62), 0.03 + 0.09 * rim);
        return;
    }
#ifndef SOLID
    if (focus && vfocus < 0.5) discard;
#endif
    vec3 l = normalize(light.xyz);
    vec3 base = toLinear(vcolor);
    if (params.y > params.x && vhigh > 0.5) {
        base = toLinear(vec3(1.0, 0.78, 0.05));
    }
    vec3 c = shade(n, l, v, base, 1.0 - flat_k);
    // The moire: a layer one or two pixels high has a bright top, a dark
    // bottom, and a thin flat face, and the pixels sample them at a phase
    // that drifts from row to row, in bands. When a layer is less than 3
    // pixels high on the screen (vh over the size of a pixel), each
    // pixel of a bead gets the mean light of its round side: 8 points from
    // the bottom to the top, each weighted by how much it faces the eye
    // (its share of the pixels). The mean is the brightness of the print
    // there, with no bands. The side is the outward direction of the bead
    // (vsec.xy), or on a flat face, the direction to the eye.
    // The size of a pixel on the surface (mm), from the change of the
    // place across it: it works on the flat faces too, where z does not
    // change.
    vec3 dpx = dFdx(vpos);
    vec3 dpy = dFdy(vpos);
    float mm_px = sqrt(0.5 * (dot(dpx, dpx) + dot(dpy, dpy)));
    float layer_px = vh / max(mm_px, 1e-6);
    float t = 1.0 - smoothstep(1.5, 3.0, layer_px);
    if (t > 0.0 && vsec.w == 0.0) {
        vec2 hv2 = dot(vsec.xy, vsec.xy) > 0.25 ? normalize(vsec.xy) : normalize(v.xy + vec2(1e-5, 0.0));
        // The side that faces the eye.
        vec2 np = dot(hv2, v.xy) < 0.0 ? -hv2 : hv2;
        vec3 acc = vec3(0.0);
        float wsum = 0.0;
        for (int k = 0; k < 8; k++) {
            float phi = 3.14159265 * ((float(k) + 0.5) / 8.0 - 0.5);
            vec3 nk = vec3(np * cos(phi), sin(phi));
            float wk = max(dot(nk, v), 0.0) + 1e-3;
            acc += wk * shade(nk, l, v, base, 1.0 - flat_k);
            wsum += wk;
        }
        c = mix(c, acc / wsum, t);
    }
    if (params.w > 2.5) {
        float rim = 1.0 - abs(dot(n, v));
        frag = vec4(toSrgb(c + vec3(0.25 * rim * rim)), 0.38 + 0.3 * rim);
        return;
    }
    frag = vec4(mix(toSrgb(c), vec3(0.965, 0.969, 0.973), fade), 1.0);
}`;

const gridVert = `#version 300 es
// A port of src/render/shaders/grid.vert.
precision highp float;
layout(location = 0) in vec3 pos;
uniform mat4 mvp;
void main() {
    gl_Position = mvp * vec4(pos, 1.0);
    gl_Position.y = -gl_Position.y;
    gl_Position.z = 2.0 * gl_Position.z - gl_Position.w;
}`;

const gridFrag = `#version 300 es
precision highp float;
uniform vec4 color;
out vec4 frag;
void main() {
    frag = vec4(color.rgb, 1.0);
}`;

// The bed model: ports of src/render/shaders/bed.vert and bed.frag.
const bedVert = `#version 300 es
precision highp float;
layout(location = 0) in vec3 pos;
layout(location = 1) in vec3 normal;
layout(location = 2) in vec2 uv;
uniform mat4 mvp;
out vec3 vnormal;
out vec2 vuv;
out float vz;
void main() {
    gl_Position = mvp * vec4(pos, 1.0);
    gl_Position.y = -gl_Position.y;
    gl_Position.z = 2.0 * gl_Position.z - gl_Position.w;
    vnormal = normal;
    vuv = uv;
    vz = pos.z;
}`;

const bedFrag = `#version 300 es
precision highp float;
uniform vec4 color;
uniform vec4 light;
uniform vec4 params;
uniform sampler2D tex;
in vec3 vnormal;
in vec2 vuv;
in float vz;
out vec4 frag;
void main() {
    vec3 n = normalize(vnormal);
    float lit = 0.7 + 0.3 * abs(dot(n, normalize(light.xyz)));
    vec3 col = color.rgb * lit;
    if (params.x > 0.5 && vuv.x > -0.5) {
        vec4 t = texture(tex, vuv);
        col = mix(col, t.rgb * lit, t.a * 0.7);
    }
    float a = (vz > params.y || params.z > 0.5) ? 0.3 : 1.0;
    frag = vec4(col, a);
}`;

// The signs of the plates: a port of src/render/shaders/sign.frag (the
// vertex shader is bedVert). The color is the image of the signs, and the
// alpha of a sign is in the normal (x).
const signFrag = `#version 300 es
precision highp float;
uniform sampler2D tex;
in vec3 vnormal;
in vec2 vuv;
in float vz;
out vec4 frag;
void main() {
    vec4 t = texture(tex, vuv);
    float a = t.a * vnormal.x;
    if (a < 0.004) discard;
    frag = vec4(t.rgb, a);
}`;

// The paint tool (paint.js): the mesh of a part, each triangle in the
// color of its filament. state: 0 the filament of the part, k the color
// k of the palette, + 128 for the region under the pointer, 255 a
// triangle with pieces (they draw in their own buffer).
const paintVert = `#version 300 es
precision highp float;
layout(location = 0) in vec3 pos;
layout(location = 1) in vec3 normal;
layout(location = 2) in float state;
uniform mat4 mvp;
out vec3 vnormal;
flat out int vstate;
void main() {
    gl_Position = mvp * vec4(pos, 1.0);
    gl_Position.y = -gl_Position.y;
    gl_Position.z = 2.0 * gl_Position.z - gl_Position.w;
    vnormal = normal;
    vstate = int(state + 0.5);
}`;

const paintFrag = `#version 300 es
precision highp float;
uniform vec3 colors[18];
uniform vec3 light;
in vec3 vnormal;
flat in int vstate;
out vec4 frag;
void main() {
    if (vstate == 255) discard;
    int k = vstate & 127;
    vec3 c = colors[min(k, 17)];
    vec3 n = normalize(vnormal);
    float lit = 0.45 + 0.55 * max(dot(n, normalize(light)), 0.0) + 0.15 * abs(n.z);
    vec3 col = c * min(lit, 1.0);
    if (vstate >= 128) col = mix(col, vec3(0.2, 0.55, 1.0), 0.45);
    frag = vec4(col, 1.0);
}`;

// The 2D view: a stroke with round ends for each bead (instanced). The
// quad of a bead goes from a - r to b + r along it, and r to each side;
// the fragment shader cuts the round ends. kind 0: the feature colors,
// with the faint colors of a focus; 1: one color; 2: a mark (only its
// color). A faint color is the color over the bed at 12 %, so all beads
// draw with no blending (a stroke that overlaps itself stays even).
const lineVert = `#version 300 es
precision highp float;
precision highp int;
layout(location = 2) in vec4 seg;
layout(location = 3) in vec3 zhw;
layout(location = 4) in vec4 color;
uniform vec4 xf;
uniform float px;
uniform float grow;
uniform float kind;
uniform float mask;
uniform float dim;
uniform vec3 solid;
out vec2 vlocal;
out float vlen;
out float vr;
flat out vec3 vcolor;

const vec3 feature_rgb[14] = vec3[14](
    vec3(0.851, 0.282, 0.059), vec3(0.961, 0.624, 0.000),
    vec3(0.110, 0.494, 0.839), vec3(0.169, 0.541, 0.243),
    vec3(0.682, 0.243, 0.788), vec3(0.553, 0.431, 0.388),
    vec3(0.678, 0.710, 0.741), vec3(0.286, 0.314, 0.341),
    vec3(0.047, 0.651, 0.682), vec3(0.510, 0.788, 0.118),
    vec3(0.400, 0.800, 0.949),
    vec3(0.878, 0.102, 0.149), vec3(0.200, 0.451, 0.949),
    vec3(0.949, 0.251, 0.749));
const vec3 bed = vec3(0.906, 0.914, 0.925);

void main() {
    uint feature = uint(color.a * 255.0 + 0.5);
    uint m = uint(mask + 0.5);
    vec2 a = seg.xy;
    vec2 b = seg.zw;
    vec2 d = b - a;
    float len = length(d);
    vec2 dir = len > 0.0 ? d / len : vec2(1.0, 0.0);
    vec2 side = vec2(-dir.y, dir.x);
    float r = max(0.5 * zhw.z * grow, px);
    // A travel move is a thin line.
    if (kind > 1.5 && feature == 12u) r = max(0.18 * zhw.z, px);
    int corner = gl_VertexID;
    float u = (corner & 1) == 0 ? -r : len + r;
    float v = corner < 2 ? -r : r;
    vec2 p = a + dir * u + side * v;
    vlocal = vec2(u, v);
    vlen = len;
    vr = r;
    if (kind > 1.5) {
        // A mark shows only in a focus on it.
        bool on = feature < 14u && ((m >> feature) & 1u) != 0u;
        vcolor = feature_rgb[min(feature, 13u)];
        gl_Position = on ? vec4(p * xf.xy + xf.zw, 0.0, 1.0) : vec4(2.0, 2.0, 2.0, 1.0);
        return;
    }
    if (kind > 0.5) {
        vcolor = solid;
    } else {
        vec3 c = feature_rgb[min(feature, 13u)];
        bool focus = m == 0u || ((m >> feature) & 1u) != 0u;
        vcolor = (dim > 0.5 || !focus) ? mix(bed, c, 0.12) : c;
    }
    gl_Position = vec4(p * xf.xy + xf.zw, 0.0, 1.0);
}`;

const lineFrag = `#version 300 es
precision highp float;
in vec2 vlocal;
in float vlen;
in float vr;
flat in vec3 vcolor;
out vec4 frag;
void main() {
    // Outside the round ends: no pixel.
    float u = vlocal.x < 0.0 ? vlocal.x : (vlocal.x > vlen ? vlocal.x - vlen : 0.0);
    if (u * u + vlocal.y * vlocal.y > vr * vr) discard;
    frag = vec4(vcolor, 1.0);
}`;

// The bed, the grid, and the outline: points in mm, in one color. `size`
// scales the points (the quad of the bed is 0 to 1).
const flatVert = `#version 300 es
precision highp float;
layout(location = 0) in vec2 pos;
uniform vec4 xf;
uniform vec2 size;
void main() {
    gl_Position = vec4(pos * size * xf.xy + xf.zw, 0.0, 1.0);
}`;

const flatFrag = `#version 300 es
precision highp float;
uniform vec4 color;
out vec4 frag;
void main() {
    frag = color;
}`;

// The bits of the marks in a focus mask (ZS_MARK_SEAM and on).
const MARK_BITS = (1 << 11) | (1 << 12) | (1 << 13);

function compile(gl, vs, fs) {
  const prog = gl.createProgram();
  for (const [type, src] of [[gl.VERTEX_SHADER, vs], [gl.FRAGMENT_SHADER, fs]]) {
    const sh = gl.createShader(type);
    gl.shaderSource(sh, src);
    gl.compileShader(sh);
    if (!gl.getShaderParameter(sh, gl.COMPILE_STATUS)) throw new Error(`shader: ${gl.getShaderInfoLog(sh)}`);
    gl.attachShader(prog, sh);
  }
  gl.linkProgram(prog);
  if (!gl.getProgramParameter(prog, gl.LINK_STATUS)) throw new Error(`program: ${gl.getProgramInfoLog(prog)}`);
  const u = {};
  const n = gl.getProgramParameter(prog, gl.ACTIVE_UNIFORMS);
  for (let i = 0; i < n; i++) {
    const name = gl.getActiveUniform(prog, i).name;
    u[name] = gl.getUniformLocation(prog, name);
  }
  return { prog, u };
}

export class GlRenderer {
  constructor(canvas) {
    const gl = canvas.getContext("webgl2", { antialias: true, alpha: false, stencil: true, preserveDrawingBuffer: true });
    if (!gl) throw new Error("This browser has no WebGL2.");
    this.gl = gl;
    // Two programs for the beads. A shader with a discard turns off the
    // early depth test on many GPUs, so each pixel of each bead is shaded,
    // also the hidden ones: zoomed in, that is most of the frame time (an
    // Intel HD 530). The solid program has no discard; it draws the beads
    // with no focus and no ribbons. The other draws a focus and ribbons.
    this.boxAll = compile(gl, boxVert, boxFrag);
    this.boxSolid = compile(gl, boxVert, boxFrag.replace("#version 300 es\n", "#version 300 es\n#define SOLID\n"));
    // The depth pass (see `draw`): the same vertex shader, no color.
    this.boxDepth = compile(gl, boxVert, "#version 300 es\nprecision mediump float;\nout vec4 frag;\nvoid main() { frag = vec4(0.0); }\n");
    this.box = this.boxAll;
    this.depthPass = true;
    // A bead is a closed mesh, wound the same way: its back faces are
    // always behind its front faces. Culling them halves the pixels of
    // both passes, with the same picture.
    this.cull = gl.BACK;
    this.grid = compile(gl, gridVert, gridFrag);
    this.bedProg = compile(gl, bedVert, bedFrag);
    this.signProg = compile(gl, bedVert, signFrag);
    // The signs of the plates: their vertices each frame, and their image.
    this.signVbo = gl.createBuffer();
    this.signVao = gl.createVertexArray();
    this.signTex = gl.createTexture();
    this.signGen = -1;
    this.signTexWidth = 0;
    this.paintProg = compile(gl, paintVert, paintFrag);
    // The paint mesh (setPaintMesh): null when the paint tool is off.
    this.paint = null;
    // The decals of the Label tool (setLabels): null for none.
    this.labels = null;
    this.bedVbo = gl.createBuffer();
    this.bedVao = gl.createVertexArray();
    this.bedTex = gl.createTexture();
    this.bedVertexCount = 0;
    this.meshVbo = gl.createBuffer();
    this.meshIbo = gl.createBuffer();
    this.instVbo = gl.createBuffer();
    this.gridVbo = gl.createBuffer();
    this.boxVao = gl.createVertexArray();
    this.gridVao = gl.createVertexArray();
    this.gen = { mesh: -1, instances: -1, grid: -1, bed: -1 };
    this.bed = new Float32Array(2);
    this.gridVertices = 0;
    this.init2d();
    this.indexCount = 0;
    // The coarser meshes (lod 1 and 2): first index and count of each.
    this.lod = new Uint32Array(4);
    // A copy of the push constants with other params (made once).
    this.push2 = new Float32Array(28);
    this.instanceCount = 0;
    this.instCapacity = 0;
    // The instance buffer and the light bead buffer of a new scene while
    // they fill (see stageBegin).
    this.stage = null;
    this.stageLight = null;
    // The bead buffers of the plates: slot -> { vbo, gen }.
    this.slots = new Map();
    // The grid lines of the other plates.
    this.plateGridVbo = gl.createBuffer();
    this.plateGridVao = gl.createVertexArray();
    this.plateGridGen = -1;
    // The push data of a plate (made once).
    this.push3 = new Float32Array(28);
    gl.bindVertexArray(this.plateGridVao);
    gl.bindBuffer(gl.ARRAY_BUFFER, this.plateGridVbo);
    gl.enableVertexAttribArray(0);
    gl.vertexAttribPointer(0, 3, gl.FLOAT, false, 12, 0);

    gl.bindVertexArray(this.gridVao);
    gl.bindBuffer(gl.ARRAY_BUFFER, this.gridVbo);
    gl.enableVertexAttribArray(0);
    gl.vertexAttribPointer(0, 3, gl.FLOAT, false, 12, 0);

    gl.bindVertexArray(this.bedVao);
    gl.bindBuffer(gl.ARRAY_BUFFER, this.bedVbo);
    gl.enableVertexAttribArray(0);
    gl.vertexAttribPointer(0, 3, gl.FLOAT, false, 32, 0);
    gl.enableVertexAttribArray(1);
    gl.vertexAttribPointer(1, 3, gl.FLOAT, false, 32, 12);
    gl.enableVertexAttribArray(2);
    gl.vertexAttribPointer(2, 2, gl.FLOAT, false, 32, 24);

    gl.bindVertexArray(this.signVao);
    gl.bindBuffer(gl.ARRAY_BUFFER, this.signVbo);
    gl.enableVertexAttribArray(0);
    gl.vertexAttribPointer(0, 3, gl.FLOAT, false, 32, 0);
    gl.enableVertexAttribArray(1);
    gl.vertexAttribPointer(1, 3, gl.FLOAT, false, 32, 12);
    gl.enableVertexAttribArray(2);
    gl.vertexAttribPointer(2, 2, gl.FLOAT, false, 32, 24);

    gl.bindVertexArray(this.boxVao);
    gl.bindBuffer(gl.ARRAY_BUFFER, this.meshVbo);
    gl.enableVertexAttribArray(0);
    gl.vertexAttribPointer(0, 4, gl.FLOAT, false, 16, 0);
    gl.bindBuffer(gl.ELEMENT_ARRAY_BUFFER, this.meshIbo);
    for (const loc of [2, 3, 4]) {
      gl.enableVertexAttribArray(loc);
      gl.vertexAttribDivisor(loc, 1);
    }
    gl.bindVertexArray(null);
  }

  // Binds the instance attributes from instance `first` on, in the bead
  // buffer `vbo` (the buffer of the active plate by default).
  bindInstances(first, vbo = this.instVbo) {
    const gl = this.gl;
    const off = first * STRIDE;
    gl.bindBuffer(gl.ARRAY_BUFFER, vbo);
    gl.vertexAttribPointer(2, 4, gl.FLOAT, false, STRIDE, off);
    gl.vertexAttribPointer(3, 3, gl.FLOAT, false, STRIDE, off + 16);
    gl.vertexAttribPointer(4, 4, gl.UNSIGNED_BYTE, true, STRIDE, off + 28);
  }

  setPush(push) {
    const gl = this.gl;
    const u = this.box.u;
    gl.uniformMatrix4fv(u.mvp, false, push, 0, 16);
    gl.uniform4fv(u.light, push, 16, 4);
    gl.uniform4fv(u.eye, push, 20, 4);
    gl.uniform4fv(u.params, push, 24, 4);
  }

  // The beads of the draw list with the full mesh: the near ones, which
  // cover the most pixels.
  fullBeads(f) {
    let n = 0;
    const d = f.draws;
    for (let i = 0; i < f.drawsCount; i++) if (d[3 * i + 2] === 0) n += d[3 * i + 1];
    return n;
  }

  // The opaque beads of a frame: the draw list, the marks, a part that
  // turns in the view only, and the other plates.
  beadPass(f, first) {
    const gl = this.gl;
    gl.uniform1f(this.box.u.fade, 0);
    this.setPush(f.box);
    this.drawList(f, first);
    this.drawBeads(f.markFirst, f.markCount);
    // A part that turns in the view only, with its own matrix.
    if (f.partDrawsCount > 0) {
      this.setPush(f.part);
      const d = f.partDraws;
      for (let i = 0; i < f.partDrawsCount; i++) this.drawBeads(d[3 * i], d[3 * i + 1], d[3 * i + 2]);
      this.setPush(f.box);
    }
    this.drawPlates(f);
  }

  // Draws the beads first .. first + count with the full mesh, or with
  // a coarser mesh (lod 1 or 2) if there is one.
  drawBeads(first, count, lod = 0, vbo = this.instVbo, base = first) {
    if (count <= 0) return;
    const gl = this.gl;
    this.bindInstances(first, vbo);
    gl.uniform1f(this.box.u.base, base);
    const n = lod > 0 ? this.lod[2 * lod - 1] : 0;
    if (n > 0) gl.drawElementsInstanced(gl.TRIANGLES, n, gl.UNSIGNED_SHORT, this.lod[2 * lod - 2] * 2, count);
    else gl.drawElementsInstanced(gl.TRIANGLES, this.indexCount, gl.UNSIGNED_SHORT, 0, count);
  }

  // The beads of a frame: its draw list (the chunks in the view, each
  // with its mesh). An empty list draws no bead: the beads are out of the
  // view (with several plates, the camera often looks at another plate).
  drawList(f, first) {
    if (f.drawsCount === 0) return;
    // The last ranges first: the top layers, which a view from above sees
    // first, so the depth test drops the hidden pixels of the layers
    // under them early.
    const d = f.draws;
    for (let i = f.drawsCount - 1; i >= 0; i--) this.drawBeads(d[3 * i], d[3 * i + 1], d[3 * i + 2]);
  }

  // The beads of the other plates, and the light beads of the active
  // plate (flag 2, or flag 4 while its part moves): each with its matrix, its fade, and its draws. With
  // `ghost` (the push data of the ghost pass of a focus): only the light
  // beads of the active plate, as ghosts.
  drawPlates(f, ghost = null) {
    if (f.platesCount === 0) return;
    const gl = this.gl;
    const m = f.m32;
    const mf = f.mf32;
    const push = this.push3;
    const d = f.plateDraws;
    for (let i = 0; i < f.platesCount; i++) {
      const q = (f.plates >>> 2) + 40 * i;
      const n = m[q + 36];
      if (n === 0) continue;
      const s = this.slots.get(m[q + 39]);
      if (!s) continue;
      if (ghost && !(m[q + 38] & 2)) continue;
      if (m[q + 38] & 4) {
        // The light beads of a part that moves in the view only: the push
        // data of the part (its matrix and its turned light).
        this.setPush(f.part);
      } else if (m[q + 38] & 2) {
        this.setPush(ghost ?? f.box);
      } else {
        // The push data of the frame, with the matrix of the plate, no
        // highlight, and no focus.
        push.set(f.box);
        for (let k = 0; k < 16; k++) push[k] = mf[q + k];
        push[24] = 0;
        push[25] = 0;
        push[26] = 0;
        this.setPush(push);
      }
      gl.uniform1f(this.box.u.fade, mf[q + 37]);
      const d0 = m[q + 35];
      // A base out of the highlight range of the active plate.
      for (let j = d0; j < d0 + n; j++) this.drawBeads(d[3 * j], d[3 * j + 1], d[3 * j + 2], s.vbo, 1e9);
    }
    gl.uniform1f(this.box.u.fade, 0);
    this.setPush(ghost ?? f.box);
  }

  // Sends the buffers of a frame that changed to the GPU.
  sync(f) {
    const gl = this.gl;
    this.gridVertices = f.gridVertices;
    if (f.meshGen !== this.gen.mesh) {
      gl.bindVertexArray(null);
      gl.bindBuffer(gl.ARRAY_BUFFER, this.meshVbo);
      gl.bufferData(gl.ARRAY_BUFFER, f.meshVerts, gl.STATIC_DRAW);
      gl.bindBuffer(gl.ELEMENT_ARRAY_BUFFER, this.meshIbo);
      gl.bufferData(gl.ELEMENT_ARRAY_BUFFER, f.meshIndex, gl.STATIC_DRAW);
      this.indexCount = f.meshFullCount || f.meshIndex.length;
      this.lod[0] = f.lod1First;
      this.lod[1] = f.lod1Count;
      this.lod[2] = f.lod2First;
      this.lod[3] = f.lod2Count;
      this.gen.mesh = f.meshGen;
    }
    const st = this.stage;
    if (st && st.gen >= 0 && st.gen === f.instancesGen && st.count === f.instanceCount) {
      // The buffer of a new scene, filled in steps: it has these
      // instances already.
      gl.deleteBuffer(this.instVbo);
      this.instVbo = st.vbo;
      this.instCapacity = st.capacity;
      this.instanceCount = f.instanceCount;
      this.gen.instances = f.instancesGen;
      this.stage = null;
    } else if (st && st.gen >= 0) {
      // The instances changed again after the scene: the staged buffer
      // is old.
      this.stageDrop();
    }
    if (f.instancesGen !== this.gen.instances && f.patchFrom === this.gen.instances && f.instanceCount === this.instanceCount) {
      // A change of a few beads (the paths of a selection): only them.
      gl.bindVertexArray(null);
      gl.bindBuffer(gl.ARRAY_BUFFER, this.instVbo);
      if (f.patchEnd > f.patchFirst) gl.bufferSubData(gl.ARRAY_BUFFER, f.patchFirst * STRIDE, f.instances, f.patchFirst * STRIDE, (f.patchEnd - f.patchFirst) * STRIDE);
      this.gen.instances = f.instancesGen;
    }
    if (f.instancesGen !== this.gen.instances) {
      gl.bindBuffer(gl.ARRAY_BUFFER, this.instVbo);
      const need = (f.instanceCount + MAX_EXTRAS) * STRIDE;
      if (need > this.instCapacity) {
        gl.bufferData(gl.ARRAY_BUFFER, need, gl.DYNAMIC_DRAW);
        this.instCapacity = need;
      }
      if (f.instances.length > 0) gl.bufferSubData(gl.ARRAY_BUFFER, 0, f.instances);
      this.instanceCount = f.instanceCount;
      this.gen.instances = f.instancesGen;
    }
    if (f.bedGen !== this.gen.bed) {
      // The bed model: static, so it goes to the GPU once for each printer.
      gl.bindVertexArray(null);
      gl.bindBuffer(gl.ARRAY_BUFFER, this.bedVbo);
      gl.bufferData(gl.ARRAY_BUFFER, f.bedVerts, gl.STATIC_DRAW);
      this.bedVertexCount = f.bedVertexCount;
      gl.bindTexture(gl.TEXTURE_2D, this.bedTex);
      gl.pixelStorei(gl.UNPACK_ALIGNMENT, 1);
      if (f.bedTexWidth > 0) gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, f.bedTexWidth, f.bedTexHeight, 0, gl.RGBA, gl.UNSIGNED_BYTE, f.bedTex);
      else gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, 1, 1, 0, gl.RGBA, gl.UNSIGNED_BYTE, new Uint8Array([255, 255, 255, 255]));
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
      this.gen.bed = f.bedGen;
    }
    if (f.signGen !== this.signGen) {
      // The image of the signs: new when a plate, its name, or its
      // status changes. Mipmaps keep the text clean when it is small.
      gl.bindTexture(gl.TEXTURE_2D, this.signTex);
      gl.pixelStorei(gl.UNPACK_ALIGNMENT, 1);
      this.signTexWidth = f.signTexWidth;
      if (f.signTexWidth > 0) {
        gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, f.signTexWidth, f.signTexHeight, 0, gl.RGBA, gl.UNSIGNED_BYTE, f.signTex);
        gl.generateMipmap(gl.TEXTURE_2D);
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR_MIPMAP_LINEAR);
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
      }
      this.signGen = f.signGen;
    }
    // The bead buffers of the plates in the frame.
    const m = f.m32;
    for (let i = 0; i < f.platesCount; i++) {
      const q = (f.plates >>> 2) + 40 * i;
      const slot = m[q + 39];
      const gen = m[q + 34];
      let s = this.slots.get(slot);
      if (!s) {
        s = { vbo: gl.createBuffer(), gen: -1 };
        this.slots.set(slot, s);
      }
      const light = this.stageLight;
      if (s.gen !== gen && light && light.gen >= 0 && m[q + 38] & 6) {
        // The light beads of the active plate: the staged buffer if it
        // has them, else it is old.
        if (light.gen === gen && light.count === m[q + 33]) {
          gl.deleteBuffer(s.vbo);
          s.vbo = light.vbo;
          s.gen = gen;
          this.stageLight = null;
        } else this.stageDropLight();
      }
      if (s.gen !== gen) {
        gl.bindVertexArray(null);
        gl.bindBuffer(gl.ARRAY_BUFFER, s.vbo);
        const n = m[q + 33];
        gl.bufferData(gl.ARRAY_BUFFER, n > 0 ? new Uint8Array(f.buf, m[q + 32], n * STRIDE) : new Uint8Array(STRIDE), gl.STATIC_DRAW);
        s.gen = gen;
      }
    }
    if (f.plateGridGen !== this.plateGridGen) {
      gl.bindVertexArray(null);
      gl.bindBuffer(gl.ARRAY_BUFFER, this.plateGridVbo);
      const n = f.plateGridVertices;
      gl.bufferData(gl.ARRAY_BUFFER, n > 0 ? new Float32Array(f.buf, f.plateGridPtr, 3 * n) : new Float32Array(3), gl.STATIC_DRAW);
      this.plateGridGen = f.plateGridGen;
    }
    if (f.gridGen !== this.gen.grid) {
      gl.bindBuffer(gl.ARRAY_BUFFER, this.gridVbo);
      gl.bufferData(gl.ARRAY_BUFFER, f.gridPoints, gl.STATIC_DRAW);
      this.gen.grid = f.gridGen;
      // The bed size: the largest x and y of the grid.
      let bx = 0;
      let by = 0;
      for (let i = 0; i < f.gridPoints.length; i += 3) {
        if (f.gridPoints[i] > bx) bx = f.gridPoints[i];
        if (f.gridPoints[i + 1] > by) by = f.gridPoints[i + 1];
      }
      this.bed[0] = bx;
      this.bed[1] = by;
    }
  }

  // ---- a new scene in steps (app.js, stageScene) ----
  //
  // The instances of a large new scene go to a second buffer in pieces,
  // a few MB in each frame, while the frames draw the old scene from the
  // first buffer. One upload of all of them blocks the main thread until
  // the GPU process has them all.

  // Buffers for `count` instances (and the spare slots of the extras),
  // and for `lightCount` light beads (the bead buffer of the light levels
  // of the active plate).
  stageBegin(count, lightCount) {
    const gl = this.gl;
    this.stageDrop();
    this.stageDropLight();
    gl.bindVertexArray(null);
    const capacity = (count + MAX_EXTRAS) * STRIDE;
    const vbo = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, vbo);
    gl.bufferData(gl.ARRAY_BUFFER, capacity, gl.DYNAMIC_DRAW);
    this.stage = { vbo, count, capacity, gen: -1 };
    if (lightCount > 0) {
      const lvbo = gl.createBuffer();
      gl.bindBuffer(gl.ARRAY_BUFFER, lvbo);
      gl.bufferData(gl.ARRAY_BUFFER, lightCount * STRIDE, gl.STATIC_DRAW);
      this.stageLight = { vbo: lvbo, count: lightCount, gen: -1 };
    }
  }

  // Bytes at byte `at` of the staged instance buffer, or of the staged
  // light bead buffer (`light`).
  stagePut(bytes, at, light = false) {
    const gl = this.gl;
    gl.bindVertexArray(null);
    gl.bindBuffer(gl.ARRAY_BUFFER, light ? this.stageLight.vbo : this.stage.vbo);
    gl.bufferSubData(gl.ARRAY_BUFFER, at, bytes);
  }

  // The view has read the scene: its instances are of generation `gen`,
  // and its light beads of `lightGen`. The next sync with these
  // generations uses the staged buffers.
  stageEnd(gen, lightGen) {
    if (this.stage) this.stage.gen = gen;
    if (this.stageLight) this.stageLight.gen = lightGen;
  }

  stageDrop() {
    if (!this.stage) return;
    this.gl.deleteBuffer(this.stage.vbo);
    this.stage = null;
  }

  stageDropLight() {
    if (!this.stageLight) return;
    this.gl.deleteBuffer(this.stageLight.vbo);
    this.stageLight = null;
  }

  // The bed model, after the beads: blended (its parts over the plate are
  // see-through), with a depth offset so the grid lines stay in front.
  // mvp: the matrix of a plate (an array and its first item), or the
  // bed of the frame.
  drawBed(f, mvp = f.bed, at = 0) {
    if (f.bedVertices === 0 || this.bedVertexCount === 0) return;
    const gl = this.gl;
    const u = this.bedProg.u;
    gl.useProgram(this.bedProg.prog);
    gl.uniformMatrix4fv(u.mvp, false, mvp, at, 16);
    gl.uniform4fv(u.color, f.bed, 16, 4);
    gl.uniform4fv(u.light, f.bed, 20, 4);
    gl.uniform4fv(u.params, f.bed, 24, 4);
    gl.activeTexture(gl.TEXTURE0);
    gl.bindTexture(gl.TEXTURE_2D, this.bedTex);
    gl.uniform1i(u.tex, 0);
    gl.bindVertexArray(this.bedVao);
    // Two passes, so each pixel blends once, with the nearest surface of
    // the bed model: first the depth only, then the color where the depth
    // is that depth. In one pass, the see-through faces (alpha 0.3) that
    // lie over each other added up in the order of the triangles, and the
    // triangles of the mesh showed.
    const count = Math.min(f.bedVertices, this.bedVertexCount);
    gl.enable(gl.POLYGON_OFFSET_FILL);
    gl.polygonOffset(2, 4);
    gl.colorMask(false, false, false, false);
    gl.drawArrays(gl.TRIANGLES, 0, count);
    gl.colorMask(true, true, true, true);
    gl.depthFunc(gl.EQUAL);
    gl.depthMask(false);
    gl.enable(gl.BLEND);
    gl.blendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA);
    gl.drawArrays(gl.TRIANGLES, 0, count);
    gl.disable(gl.BLEND);
    gl.depthMask(true);
    gl.depthFunc(gl.LEQUAL);
    gl.disable(gl.POLYGON_OFFSET_FILL);
  }

  // The signs of the plates, after the bed: blended, with no depth
  // write, so the parts in front hide them and they hide no part.
  drawSigns(f) {
    if (!(f.signVertices > 0) || this.signTexWidth === 0) return;
    const gl = this.gl;
    gl.useProgram(this.signProg.prog);
    gl.uniformMatrix4fv(this.signProg.u.mvp, false, f.sign, 0, 16);
    gl.activeTexture(gl.TEXTURE0);
    gl.bindTexture(gl.TEXTURE_2D, this.signTex);
    gl.uniform1i(this.signProg.u.tex, 0);
    gl.bindVertexArray(this.signVao);
    gl.bindBuffer(gl.ARRAY_BUFFER, this.signVbo);
    gl.bufferData(gl.ARRAY_BUFFER, f.signVerts, gl.STREAM_DRAW);
    gl.enable(gl.BLEND);
    gl.blendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA);
    gl.depthMask(false);
    gl.drawArrays(gl.TRIANGLES, 0, f.signVertices);
    gl.depthMask(true);
    gl.disable(gl.BLEND);
  }

  // ---- the paint tool (paint.js) ----

  // The mesh of a part for the paint tool: pos (9 floats for each
  // triangle) and normals (3 floats for each triangle). null ends it.
  setPaintMesh(pos, normals) {
    const gl = this.gl;
    if (this.paint) {
      for (const b of [this.paint.vbo, this.paint.sbo, this.paint.lvbo]) gl.deleteBuffer(b);
      for (const v of [this.paint.vao, this.paint.lvao]) gl.deleteVertexArray(v);
    }
    this.paint = null;
    if (!pos) return;
    const n = pos.length / 9;
    const data = new Float32Array(n * 18);
    for (let t = 0; t < n; t++) {
      for (let k = 0; k < 3; k++) {
        const o = (3 * t + k) * 6;
        data[o] = pos[9 * t + 3 * k];
        data[o + 1] = pos[9 * t + 3 * k + 1];
        data[o + 2] = pos[9 * t + 3 * k + 2];
        data[o + 3] = normals[3 * t];
        data[o + 4] = normals[3 * t + 1];
        data[o + 5] = normals[3 * t + 2];
      }
    }
    const p = { count: 3 * n, leaves: 0 };
    p.vao = gl.createVertexArray();
    gl.bindVertexArray(p.vao);
    p.vbo = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, p.vbo);
    gl.bufferData(gl.ARRAY_BUFFER, data, gl.STATIC_DRAW);
    gl.enableVertexAttribArray(0);
    gl.vertexAttribPointer(0, 3, gl.FLOAT, false, 24, 0);
    gl.enableVertexAttribArray(1);
    gl.vertexAttribPointer(1, 3, gl.FLOAT, false, 24, 12);
    p.sbo = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, p.sbo);
    gl.bufferData(gl.ARRAY_BUFFER, 3 * n, gl.DYNAMIC_DRAW);
    gl.enableVertexAttribArray(2);
    gl.vertexAttribPointer(2, 1, gl.UNSIGNED_BYTE, false, 1, 0);
    // The pieces of the split triangles: pos, normal, and state.
    p.lvao = gl.createVertexArray();
    gl.bindVertexArray(p.lvao);
    p.lvbo = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, p.lvbo);
    gl.enableVertexAttribArray(0);
    gl.vertexAttribPointer(0, 3, gl.FLOAT, false, 28, 0);
    gl.enableVertexAttribArray(1);
    gl.vertexAttribPointer(1, 3, gl.FLOAT, false, 28, 12);
    gl.enableVertexAttribArray(2);
    gl.vertexAttribPointer(2, 1, gl.FLOAT, false, 28, 24);
    gl.bindVertexArray(null);
    this.paint = p;
    this.paintColors = new Float32Array(54);
  }

  // The state of each vertex (3 for each triangle, see paintFrag).
  setPaintStates(states) {
    if (!this.paint) return;
    const gl = this.gl;
    gl.bindBuffer(gl.ARRAY_BUFFER, this.paint.sbo);
    gl.bufferSubData(gl.ARRAY_BUFFER, 0, states);
  }

  // The pieces: 7 floats for each vertex (pos, normal, state).
  setPaintLeaves(data) {
    if (!this.paint) return;
    const gl = this.gl;
    gl.bindBuffer(gl.ARRAY_BUFFER, this.paint.lvbo);
    gl.bufferData(gl.ARRAY_BUFFER, data, gl.DYNAMIC_DRAW);
    this.paint.leaves = data.length / 7;
  }

  // The colors: 18 of [r, g, b] in 0..1 (0: the filament of the part).
  setPaintColors(list) {
    if (!this.paint) return;
    for (let i = 0; i < 18; i++) {
      const c = list[i] ?? list[0];
      this.paintColors.set(c, 3 * i);
    }
  }

  drawPaint(f) {
    const gl = this.gl;
    const p = this.paint;
    gl.useProgram(this.paintProg.prog);
    gl.uniformMatrix4fv(this.paintProg.u.mvp, false, f.grid, 0, 16);
    gl.uniform3fv(this.paintProg.u["colors[0]"], this.paintColors);
    gl.uniform3f(this.paintProg.u.light, -0.4, -0.6, 0.7);
    gl.bindVertexArray(p.vao);
    gl.drawArrays(gl.TRIANGLES, 0, p.count);
    if (p.leaves > 0) {
      gl.bindVertexArray(p.lvao);
      gl.drawArrays(gl.TRIANGLES, 0, p.leaves);
    }
    gl.bindVertexArray(null);
  }

  // ---- the labels (label.js) ----

  // The live preview of the labels of the Label tool: a decal for each
  // label, on the surface of the part. list: [{ pts (Float32Array of x, y,
  // z points in the frame of the bed), ends (Uint32Array: the end of each
  // path, in points), color [r, g, b], edge [r, g, b] or null (an outline
  // of the paths) }], or null for none. The paths fill by the nonzero
  // rule in the stencil buffer, so the holes of the letters stay open.
  setLabels(list) {
    const gl = this.gl;
    if (!this.labelVbo) {
      this.labelVbo = gl.createBuffer();
      this.labelVao = gl.createVertexArray();
      gl.bindVertexArray(this.labelVao);
      gl.bindBuffer(gl.ARRAY_BUFFER, this.labelVbo);
      gl.enableVertexAttribArray(0);
      gl.vertexAttribPointer(0, 3, gl.FLOAT, false, 12, 0);
      gl.bindVertexArray(null);
    }
    this.labels = null;
    if (!list || list.length === 0) return;
    let n = 0;
    for (const l of list) n += l.pts.length / 3;
    const data = new Float32Array(3 * n);
    const draws = [];
    let at = 0;
    for (const l of list) {
      data.set(l.pts, 3 * at);
      const paths = [];
      let start = 0;
      for (const end of l.ends) {
        if (end - start >= 3) paths.push([at + start, end - start]);
        start = end;
      }
      draws.push({ paths, color: l.color, edge: l.edge });
      at += l.pts.length / 3;
    }
    gl.bindVertexArray(null);
    gl.bindBuffer(gl.ARRAY_BUFFER, this.labelVbo);
    gl.bufferData(gl.ARRAY_BUFFER, data, gl.DYNAMIC_DRAW);
    this.labels = draws;
  }

  drawLabels(f) {
    const gl = this.gl;
    const u = this.grid.u;
    gl.useProgram(this.grid.prog);
    gl.uniformMatrix4fv(u.mvp, false, f.grid, 0, 16);
    gl.bindVertexArray(this.labelVao);
    gl.enable(gl.STENCIL_TEST);
    gl.enable(gl.POLYGON_OFFSET_FILL);
    gl.polygonOffset(-2, -4);
    gl.depthMask(false);
    for (const l of this.labels) {
      gl.clearStencil(0);
      gl.clear(gl.STENCIL_BUFFER_BIT);
      // The stencil counts the turns of the fans of all paths.
      gl.colorMask(false, false, false, false);
      gl.stencilFunc(gl.ALWAYS, 0, 0xff);
      gl.stencilOpSeparate(gl.FRONT, gl.KEEP, gl.KEEP, gl.INCR_WRAP);
      gl.stencilOpSeparate(gl.BACK, gl.KEEP, gl.KEEP, gl.DECR_WRAP);
      for (const [first, count] of l.paths) gl.drawArrays(gl.TRIANGLE_FAN, first, count);
      // The color where the count is not 0.
      gl.colorMask(true, true, true, true);
      gl.stencilFunc(gl.NOTEQUAL, 0, 0xff);
      gl.stencilOp(gl.ZERO, gl.ZERO, gl.ZERO);
      gl.uniform4f(u.color, l.color[0], l.color[1], l.color[2], 1);
      for (const [first, count] of l.paths) gl.drawArrays(gl.TRIANGLE_FAN, first, count);
    }
    gl.disable(gl.STENCIL_TEST);
    gl.disable(gl.POLYGON_OFFSET_FILL);
    // The outline of the selected label.
    for (const l of this.labels) {
      if (!l.edge) continue;
      gl.uniform4f(u.color, l.edge[0], l.edge[1], l.edge[2], 1);
      for (const [first, count] of l.paths) gl.drawArrays(gl.LINE_LOOP, first, count);
    }
    gl.depthMask(true);
    gl.bindVertexArray(null);
  }

  // Draws one frame of view.js (View3D.frame).
  draw(f) {
    const gl = this.gl;
    this.sync(f);

    gl.viewport(0, 0, f.width, f.height);
    gl.clearColor(f.clear[0], f.clear[1], f.clear[2], 1);
    gl.clearDepth(1);
    gl.clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT);
    gl.enable(gl.DEPTH_TEST);
    gl.depthFunc(gl.LEQUAL);
    gl.disable(gl.CULL_FACE);
    gl.disable(gl.BLEND);
    gl.depthMask(true);

    if (f.gridVertices > 0) {
      gl.useProgram(this.grid.prog);
      gl.uniformMatrix4fv(this.grid.u.mvp, false, f.grid, 0, 16);
      gl.uniform4fv(this.grid.u.color, f.grid, 16, 4);
      gl.bindVertexArray(this.gridVao);
      gl.drawArrays(gl.LINES, 0, f.gridVertices);
    }
    // The grids of the other plates: the plates that cannot slice in a
    // lighter color.
    if (f.plateGridVertices > 0) {
      gl.useProgram(this.grid.prog);
      gl.uniformMatrix4fv(this.grid.u.mvp, false, f.grid, 0, 16);
      gl.uniform4fv(this.grid.u.color, f.grid, 16, 4);
      gl.bindVertexArray(this.plateGridVao);
      if (f.plateGridDim > 0) gl.drawArrays(gl.LINES, 0, f.plateGridDim);
      if (f.plateGridVertices > f.plateGridDim) {
        gl.uniform4f(this.grid.u.color, 0.88, 0.89, 0.91, 1);
        gl.drawArrays(gl.LINES, f.plateGridDim, f.plateGridVertices - f.plateGridDim);
      }
    }

    // The paint tool shows the mesh of the part, and no beads.
    if (this.paint) {
      this.drawPaint(f);
      this.drawBed(f);
      gl.bindVertexArray(null);
      return;
    }
    if (this.indexCount === 0 || this.instCapacity === 0) {
      this.drawBed(f);
      if (this.labels) this.drawLabels(f);
      gl.bindVertexArray(null);
      return;
    }
    // The solid program with no focus (see the constructor). The web view
    // has no ribbons (zs_view_set_ribbons stays off).
    const solid = f.box[26] === 0;
    const first = Math.min(f.boxFirst, f.boxCount);
    gl.bindVertexArray(this.boxVao);
    // Zoomed in, the beads behind the front ones cover the screen many
    // times, and the light of each pixel is most of the frame time. So
    // first a pass with depth only, then the color pass shades only the
    // front pixel of each place (the depth test drops the rest before
    // the fragment shader). The depth of the two passes is the same, so
    // the picture is the same.
    if (this.cull) {
      gl.enable(gl.CULL_FACE);
      gl.cullFace(this.cull);
    }
    if (solid && this.depthPass && this.fullBeads(f) >= DEPTH_PASS_BEADS) {
      this.box = this.boxDepth;
      gl.useProgram(this.box.prog);
      gl.colorMask(false, false, false, false);
      this.beadPass(f, first);
      gl.colorMask(true, true, true, true);
    }
    this.box = solid ? this.boxSolid : this.boxAll;
    gl.useProgram(this.box.prog);
    this.beadPass(f, first);
    gl.disable(gl.CULL_FACE);
    // With a focus: the beads out of it, as ghosts (params.w = 2).
    if (f.box[26] !== 0) {
      const ghost = this.push2;
      ghost.set(f.box);
      ghost[27] = 2;
      gl.enable(gl.BLEND);
      gl.blendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA);
      gl.depthMask(false);
      this.setPush(ghost);
      this.drawList(f, first);
      this.drawPlates(f, ghost);
      this.drawBeads(f.markFirst, f.markCount);
      gl.disable(gl.BLEND);
      gl.depthMask(true);
      this.setPush(f.box);
    }
    this.drawBed(f);
    // The bed model at each other plate of the same printer.
    for (let i = 0; i < f.platesCount; i++) {
      const q = (f.plates >>> 2) + 40 * i;
      if (f.m32[q + 38] & 1) this.drawBed(f, f.mf32, q + 16);
    }
    this.drawSigns(f);
    if (this.labels) this.drawLabels(f);
    gl.useProgram(this.box.prog);
    gl.bindVertexArray(this.boxVao);
    this.setPush(f.box);
    // The extras and the toolhead, in the spare slots after the instances.
    gl.bindBuffer(gl.ARRAY_BUFFER, this.instVbo);
    if (f.extrasCount > 0) {
      gl.bufferSubData(gl.ARRAY_BUFFER, this.instanceCount * STRIDE, f.extras);
      this.drawBeads(this.instanceCount, f.extrasCount);
    }
    if (f.headCount > 0) {
      const at = this.instanceCount + f.extrasCount;
      gl.bindBuffer(gl.ARRAY_BUFFER, this.instVbo);
      gl.bufferSubData(gl.ARRAY_BUFFER, at * STRIDE, f.head);
      const see = this.push2;
      see.set(f.box);
      see[26] = 0;
      see[27] = 3;
      gl.enable(gl.BLEND);
      gl.blendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA);
      gl.depthMask(false);
      this.setPush(see);
      this.drawBeads(at, f.headCount);
      gl.disable(gl.BLEND);
      gl.depthMask(true);
    }
    gl.bindVertexArray(null);
  }

  // ---- the 2D layer view ----
  //
  // One layer from the top, as linux/src/ui/layer-draw.c draws it: the bed
  // with its 10 mm grid, the outline of the layer in gray, and each bead
  // as a stroke with round ends in its feature color. The beads are the
  // instances of the 3D view on the GPU (the same buffer), so a new layer
  // only changes the range of a draw. The outlines go to the GPU once for
  // each slice, as fans of triangles; the stencil buffer fills them by the
  // nonzero rule.

  init2d() {
    const gl = this.gl;
    this.line = compile(gl, lineVert, lineFrag);
    this.flat = compile(gl, flatVert, flatFrag);
    this.lineVao = gl.createVertexArray();
    gl.bindVertexArray(this.lineVao);
    gl.bindBuffer(gl.ARRAY_BUFFER, this.instVbo);
    for (const loc of [2, 3, 4]) {
      gl.enableVertexAttribArray(loc);
      gl.vertexAttribDivisor(loc, 1);
    }
    // The outline: x, y points, and the triangles of the fans.
    this.outlineVbo = gl.createBuffer();
    this.outlineIbo = gl.createBuffer();
    this.outlineVao = gl.createVertexArray();
    gl.bindVertexArray(this.outlineVao);
    gl.bindBuffer(gl.ARRAY_BUFFER, this.outlineVbo);
    gl.enableVertexAttribArray(0);
    gl.vertexAttribPointer(0, 2, gl.FLOAT, false, 8, 0);
    gl.bindBuffer(gl.ELEMENT_ARRAY_BUFFER, this.outlineIbo);
    // The bed: one quad, 0 to 1 (scaled by a uniform).
    this.quadVbo = gl.createBuffer();
    this.quadVao = gl.createVertexArray();
    gl.bindVertexArray(this.quadVao);
    gl.bindBuffer(gl.ARRAY_BUFFER, this.quadVbo);
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([0, 0, 1, 0, 0, 1, 1, 1]), gl.STATIC_DRAW);
    gl.enableVertexAttribArray(0);
    gl.vertexAttribPointer(0, 2, gl.FLOAT, false, 8, 0);
    // The grid of the 3D view (vec3 points): its x and y.
    this.grid2dVao = gl.createVertexArray();
    gl.bindVertexArray(this.grid2dVao);
    gl.bindBuffer(gl.ARRAY_BUFFER, this.gridVbo);
    gl.enableVertexAttribArray(0);
    gl.vertexAttribPointer(0, 2, gl.FLOAT, false, 12, 0);
    gl.bindVertexArray(null);
    // The index range of the triangles of each layer.
    this.outlineLayerEnd = new Uint32Array(0);
    // The outlines of a new slice, until the next draw of the 2D view.
    this.outlinesPending = null;
    // The gaps of the top layer: two triangles for each run of cells.
    this.gapVbo = gl.createBuffer();
    this.gapVao = gl.createVertexArray();
    gl.bindVertexArray(this.gapVao);
    gl.bindBuffer(gl.ARRAY_BUFFER, this.gapVbo);
    gl.enableVertexAttribArray(0);
    gl.vertexAttribPointer(0, 2, gl.FLOAT, false, 8, 0);
    gl.bindVertexArray(null);
    this.gapVertices = 0;
  }

  // The gaps of the layer of the 2D view: runs (y, x0, x1 for each, mm) of
  // cells `cell` mm tall; null: none.
  setGaps2d(runs, cell) {
    const gl = this.gl;
    const n = runs ? runs.length / 3 : 0;
    this.gapVertices = n * 6;
    if (n === 0) return;
    const v = new Float32Array(n * 12);
    const h = cell / 2;
    for (let i = 0; i < n; i++) {
      const y = runs[i * 3];
      // A run of one cell is still a cell wide.
      const x0 = runs[i * 3 + 1] - h;
      const x1 = Math.max(runs[i * 3 + 2], runs[i * 3 + 1]) + h;
      v.set([x0, y - h, x1, y - h, x0, y + h, x1, y - h, x1, y + h, x0, y + h], i * 12);
    }
    gl.bindVertexArray(null);
    gl.bindBuffer(gl.ARRAY_BUFFER, this.gapVbo);
    gl.bufferData(gl.ARRAY_BUFFER, v, gl.DYNAMIC_DRAW);
  }

  // The outlines of all layers (View3D.outlines). The fans go to the GPU
  // at the next draw of the 2D view: after a slice in the 3D view, the
  // fans of nine Benchies took 20 ms of the frame of the new scene.
  setOutlines(o) {
    this.outlinesPending = o;
  }

  // The fan of each ring of the outlines of setOutlines.
  buildOutlines(o) {
    const gl = this.gl;
    let tris = 0;
    for (let r = 0; r < o.ringEnds.length; r++) {
      const n = o.ringEnds[r] - (r > 0 ? o.ringEnds[r - 1] : 0);
      if (n >= 3) tris += n - 2;
    }
    const index = new Uint32Array(tris * 3);
    const layerEnd = new Uint32Array(o.layerEnds.length);
    let at = 0;
    let ring = 0;
    for (let l = 0; l < o.layerEnds.length; l++) {
      for (; ring < o.layerEnds[l]; ring++) {
        const a = ring > 0 ? o.ringEnds[ring - 1] : 0;
        const b = o.ringEnds[ring];
        for (let k = a + 1; k + 1 < b; k++) {
          index[at++] = a;
          index[at++] = k;
          index[at++] = k + 1;
        }
      }
      layerEnd[l] = at;
    }
    gl.bindVertexArray(null);
    gl.bindBuffer(gl.ARRAY_BUFFER, this.outlineVbo);
    gl.bufferData(gl.ARRAY_BUFFER, o.points, gl.STATIC_DRAW);
    gl.bindBuffer(gl.ELEMENT_ARRAY_BUFFER, this.outlineIbo);
    gl.bufferData(gl.ELEMENT_ARRAY_BUFFER, index, gl.STATIC_DRAW);
    this.outlineLayerEnd = layerEnd;
  }

  // Binds the instance attributes of the 2D program from instance `first`.
  bindLines(first) {
    const gl = this.gl;
    const off = first * STRIDE;
    gl.vertexAttribPointer(2, 4, gl.FLOAT, false, STRIDE, off);
    gl.vertexAttribPointer(3, 3, gl.FLOAT, false, STRIDE, off + 16);
    gl.vertexAttribPointer(4, 4, gl.UNSIGNED_BYTE, true, STRIDE, off + 28);
  }

  // Draws the instances first to end as strokes: kind 0 in the feature
  // colors, else in one color (r, g, b) with the width times `grow`.
  lines(first, end, kind, grow, r, g, b) {
    if (end <= first) return;
    const gl = this.gl;
    const u = this.line.u;
    gl.uniform1f(u.kind, kind);
    gl.uniform1f(u.grow, grow);
    gl.uniform3f(u.solid, r, g, b);
    this.bindLines(first);
    gl.drawArraysInstanced(gl.TRIANGLE_STRIP, 0, 4, end - first);
  }

  // Draws the 2D view. s: { width, height, scale, ox, oy (device pixels;
  // a point x, y mm is at ox + x * scale, oy + y * scale from the bottom
  // left), layer, beads [first, end), marks [first, end), mask (the focus
  // bits), dim (1: all beads faint), sel [first, end) of the selected
  // path or [0, 0], seg (the bead of the selected segment, or -1) }.
  draw2d(s) {
    const gl = this.gl;
    if (this.outlinesPending) {
      this.buildOutlines(this.outlinesPending);
      this.outlinesPending = null;
    }
    const sx = (2 * s.scale) / s.width;
    const sy = (2 * s.scale) / s.height;
    const tx = (2 * s.ox) / s.width - 1;
    const ty = (2 * s.oy) / s.height - 1;
    gl.viewport(0, 0, s.width, s.height);
    // this.dark: the dark mode of the system (app.js).
    const d = !!this.dark;
    if (d) gl.clearColor(0.114, 0.114, 0.125, 1);
    else gl.clearColor(0.965, 0.969, 0.973, 1);
    gl.clearStencil(0);
    gl.clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT | gl.STENCIL_BUFFER_BIT);
    gl.disable(gl.DEPTH_TEST);
    gl.disable(gl.CULL_FACE);
    gl.disable(gl.BLEND);
    gl.depthMask(false);

    // The bed and its grid.
    const fu = this.flat.u;
    gl.useProgram(this.flat.prog);
    gl.uniform4f(fu.xf, sx, sy, tx, ty);
    gl.uniform2f(fu.size, this.bed[0], this.bed[1]);
    if (d) gl.uniform4f(fu.color, 0.16, 0.16, 0.18, 1);
    else gl.uniform4f(fu.color, 0.906, 0.914, 0.925, 1);
    gl.bindVertexArray(this.quadVao);
    gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
    gl.uniform2f(fu.size, 1, 1);
    if (d) gl.uniform4f(fu.color, 0.25, 0.26, 0.28, 1);
    else gl.uniform4f(fu.color, 0.82, 0.84, 0.86, 1);
    gl.bindVertexArray(this.grid2dVao);
    if (this.gridVertices > 0) gl.drawArrays(gl.LINES, 0, this.gridVertices);

    // The outline: the stencil counts the turns of the fans, then the
    // area with a count other than 0 gets the color (and a count of 0).
    const l = s.layer;
    if (l < this.outlineLayerEnd.length) {
      const i0 = l > 0 ? this.outlineLayerEnd[l - 1] : 0;
      const i1 = this.outlineLayerEnd[l];
      if (i1 > i0) {
        gl.bindVertexArray(this.outlineVao);
        gl.enable(gl.STENCIL_TEST);
        gl.colorMask(false, false, false, false);
        gl.stencilFunc(gl.ALWAYS, 0, 0xff);
        gl.stencilOpSeparate(gl.FRONT, gl.KEEP, gl.KEEP, gl.INCR_WRAP);
        gl.stencilOpSeparate(gl.BACK, gl.KEEP, gl.KEEP, gl.DECR_WRAP);
        gl.drawElements(gl.TRIANGLES, i1 - i0, gl.UNSIGNED_INT, i0 * 4);
        gl.colorMask(true, true, true, true);
        gl.stencilFunc(gl.NOTEQUAL, 0, 0xff);
        gl.stencilOp(gl.ZERO, gl.ZERO, gl.ZERO);
        if (d) gl.uniform4f(fu.color, 0.30, 0.31, 0.34, 1);
        else gl.uniform4f(fu.color, 0.80, 0.83, 0.86, 1);
        gl.drawElements(gl.TRIANGLES, i1 - i0, gl.UNSIGNED_INT, i0 * 4);
        gl.disable(gl.STENCIL_TEST);
      }
    }

    // The beads.
    if (this.instCapacity === 0) {
      gl.bindVertexArray(null);
      return;
    }
    const u = this.line.u;
    gl.useProgram(this.line.prog);
    gl.uniform4f(u.xf, sx, sy, tx, ty);
    // Half a pixel in mm: the smallest half width of a stroke, so a thin
    // line stays visible when the view is far out.
    gl.uniform1f(u.px, 0.5 / s.scale);
    gl.uniform1f(u.mask, s.mask);
    gl.uniform1f(u.dim, s.dim);
    gl.bindVertexArray(this.lineVao);
    gl.bindBuffer(gl.ARRAY_BUFFER, this.instVbo);
    const sel = s.sel;
    // The selected path: a wide yellow line under the paths.
    if (sel[1] > sel[0]) this.lines(sel[0], sel[1], 1, 2.5, 1.0, 0.82, 0.1);
    this.lines(s.beads[0], s.beads[1], 0, 1, 0, 0, 0);
    // The marks of a focus on them (seams, travel moves, retractions).
    if (s.mask & MARK_BITS) this.lines(s.marks[0], s.marks[1], 2, 1, 0, 0, 0);
    // The selected path again, on top, and its segment in orange.
    if (sel[1] > sel[0]) {
      this.lines(sel[0], sel[1], 1, 1, 1.0, 0.78, 0.05);
      if (s.seg >= 0) this.lines(s.seg, s.seg + 1, 1, 1.6, 1.0, 0.35, 0.0);
    }
    // The gaps, red, over the lines.
    if (s.gaps && this.gapVertices > 0) {
      gl.useProgram(this.flat.prog);
      gl.uniform4f(fu.xf, sx, sy, tx, ty);
      gl.uniform2f(fu.size, 1, 1);
      gl.uniform4f(fu.color, 0.91, 0.11, 0.14, 1);
      gl.bindVertexArray(this.gapVao);
      gl.drawArrays(gl.TRIANGLES, 0, this.gapVertices);
    }
    gl.bindVertexArray(null);
  }
}
