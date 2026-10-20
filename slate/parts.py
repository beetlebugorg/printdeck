# Printdeck Slate: the printed parts, the hardware, the plug spaces, and the cables, in
# build123d. All shapes are in slab coordinates (see shape.py); LOC moves them to the world.
#
# Joints. The chin snaps onto the slab with two tongues. The back plate slides up into grooves
# in the slab and the chin; the chin holds it. The foot snaps onto the chin. Every groove,
# pocket, and hole has 45 degree faces where it would overhang, so no part needs supports.

import math

from build123d import (Align, Box, Circle, Cylinder, Line, Location, Plane, Polygon, Pos, Rectangle, Rot, TangentArc,
                       Torus, Vector, Wire, chamfer, extrude, fillet, scale, sweep, Axis, Cone, RegularPolygon)

import shape as S

LOC = Location(Plane(origin=(0, 0, S.FOOT_H), x_dir=(1, 0, 0), z_dir=S.normal()))
MIN = (Align.MIN, Align.MIN, Align.MIN)
A_OUT = S.W / 2                     # outer face of a side wall
A_IN = S.W / 2 - S.WALL             # inner face of a side wall (behind the step)
A_THICK = A_IN - S.STEP_T           # inner face of a side wall in front of the step
A_PLATE = A_IN + S.GROOVE - S.CLR   # edge of the back plate, in the grooves


# ---------------------------------------------------------------- helpers


def box(a0, a1, b0, b1, c0, c1):
    a0, a1 = sorted((a0, a1))
    b0, b1 = sorted((b0, b1))
    c0, c1 = sorted((c0, c1))
    return Pos(a0, b0, c0) * Box(a1 - a0, b1 - b0, c1 - c0, align=MIN)


def prism_a(pts_bc, a0, a1):
    """A polygon in the (b, c) plane, from a0 to a1."""
    a0, a1 = sorted((a0, a1))
    pl = Plane(origin=(a0, 0, 0), x_dir=(0, 1, 0), z_dir=(1, 0, 0))
    return extrude(pl * Polygon(*pts_bc, align=None), amount=a1 - a0, dir=pl.z_dir)


def prism_b(pts_ca, b0, b1):
    """A polygon in the (c, a) plane, from b0 to b1."""
    b0, b1 = sorted((b0, b1))
    pl = Plane(origin=(0, b0, 0), x_dir=(0, 0, 1), z_dir=(0, 1, 0))
    return extrude(pl * Polygon(*pts_ca, align=None), amount=b1 - b0, dir=pl.z_dir)


def prism_c(pts_ab, c0, c1):
    """A polygon in the (a, b) plane, from c0 to c1."""
    c0, c1 = sorted((c0, c1))
    pl = Plane(origin=(0, 0, c0), x_dir=(1, 0, 0), z_dir=(0, 0, 1))
    return extrude(pl * Polygon(*pts_ab, align=None), amount=c1 - c0, dir=pl.z_dir)


def cyl_c(a, b, r, c0, c1):
    c0, c1 = sorted((c0, c1))
    return Pos(a, b, c0) * Cylinder(r, c1 - c0, align=(Align.CENTER, Align.CENTER, Align.MIN))


def round_top(shape, top_b, r):
    """Round the two edges along c at the top corners."""
    edges = shape.edges().filter_by(Axis.Z).filter_by(lambda e: abs(e.center().Y - top_b) < 0.01)
    return fillet(edges, r)


def round_slot(a0, a1, b0, b1, c0, c1):
    """A slot along a with round ends."""
    s = box(a0, a1, b0, b1, c0, c1)
    return fillet(s.edges().filter_by(Axis.Z), (b1 - b0) / 2 - 0.05)


def diamond_a(b, c, h, a0, a1):
    """Diamond pocket in a side wall: centre (b, c), half size h, from a0 to a1."""
    return prism_a([(b - h, c), (b, c - h), (b + h, c), (b, c + h)], a0, a1)


def world_below(z):
    """Everything below the world height z, in slab coordinates."""
    return LOC.inverse() * (Pos(0, 0, z - 500) * Box(1000, 1000, 1000))


def to_world(s):
    return LOC * s


def mirror_a(s):
    return s.mirror(Plane.YZ)


def both_sides(make):
    """make(sign) builds the part for one side; sign is +1 (right) or -1 (left)."""
    return [make(1), make(-1)]


def union(shapes):
    out = shapes[0]
    for s in shapes[1:]:
        out = out + s
    return out


# ---------------------------------------------------------------- slab and chin


def outer(b0, b1, top):
    """The outer box from b0 to b1. Chamfer on the front and rear edges, except on the
    edges at the joint between the slab and the chin."""
    s = box(-A_OUT, A_OUT, b0, b1, -S.D, 0)
    if top:
        s = round_top(s, b1, S.R_TOP)
    joint = (b0,) if top else (b1,)
    faces = [s.faces().sort_by(Axis.Z)[-1], s.faces().sort_by(Axis.Z)[0]]
    edges = [e for f in faces for e in f.edges()
             if not any(abs(e.start_point().Y - j) < 0.01 and abs(e.end_point().Y - j) < 0.01 for j in joint)
             and not (abs(e.start_point().Y - b0) < 0.01 and abs(e.end_point().Y - b0) < 0.01 and not top)]
    return chamfer(edges, S.EDGE)


def outline(half, top, r, c, bottom=-60.0):
    """A face in the (a, b) plane at depth c: a box with two round top corners."""
    f = Pos(0, bottom) * Rectangle(2 * half, top - bottom, align=(Align.CENTER, Align.MIN))
    f = fillet(f.vertices().group_by(Axis.Y)[-1], r)
    return Plane(origin=(0, 0, c), x_dir=(1, 0, 0), z_dir=(0, 0, 1)) * f


def groove():
    """The groove for the back plate, in the side walls and the top wall of the slab and in
    the side walls of the chin. Its rear face (the top face in print) is a 45 degree roof."""
    g = S.GROOVE
    ri = S.R_TOP - S.WALL
    c0, c1 = S.GROOVE_C
    f = outline(A_IN + g, S.H - S.WALL + g, ri + g, c0)
    return (extrude(f, amount=c1 - c0, dir=(0, 0, 1))
            + extrude(f, amount=g, dir=(0, 0, -1), taper=45))


def slab():
    b0 = S.CHIN_B
    s = outer(b0, S.H, True)
    rear = round_top(box(-A_IN, A_IN, b0 - 1, S.H - S.WALL, -S.D - 1, S.STEP_C), S.H - S.WALL, S.R_TOP - S.WALL)
    t = S.WALL + S.STEP_T
    front = round_top(box(-A_THICK, A_THICK, b0 - 1, S.H - t, S.STEP_C - 0.01, -S.WALL), S.H - t, S.R_TOP - t)
    s = s - rear - front - groove()
    # The LCD lies in a pocket in the skin; the glass is 1 mm behind the face.
    a, b, c = S.DISP_C
    s = s - box(-S.LCD_WH[0] / 2, S.LCD_WH[0] / 2, b - S.LCD_WH[1] / 2, b + S.LCD_WH[1] / 2, -S.WALL - 0.01, c)
    s = s - box(-S.OPENING_WH[0] / 2, S.OPENING_WH[0] / 2, b - S.OPENING_WH[1] / 2, b + S.OPENING_WH[1] / 2, -S.WALL, 1)
    # Bosses for the display: M3 heat-set inserts, the board ears sit on them.
    board_front = c - 7.35
    for x, y in S.EARS:
        s = s + cyl_c(x, b + y, S.BOSS_R, board_front, -S.WALL + 0.01)
        s = s - cyl_c(x, b + y, S.INSERT_D / 2, board_front - 0.01, board_front + S.INSERT_DEPTH)
    # Grooves for the ridges of the chin tongues, in the thick front part of the walls, and a
    # release hole from each groove to the outside.
    for sg in (1, -1):
        s = s - box(sg * (A_THICK - 0.1), sg * (A_THICK + 0.9), S.RIDGE_B - 0.2, S.RIDGE_B + 2.3, -3.0, S.STEP_C - 0.1)
        s = s - diamond_a(S.RIDGE_B + 1.05, -13.0, 1.0, sg * (A_THICK + 0.5), sg * (A_OUT + 1))
    return s


def chin():
    s = outer(-40, S.CHIN_B, False)
    s = s - world_below(S.FOOT_H)
    cav = box(-A_IN, A_IN, -50, S.CHIN_B + 1, -S.D - 1, -S.WALL) - world_below(S.FOOT_H + S.WALL)
    # The grooves for the back plate. The plate stops above the floor, so the grooves stop at
    # the floor too: a groove through the floor leaves a long bridge at the rear edge.
    s = s - cav - (groove() - world_below(S.FOOT_H + S.WALL))
    # Vent slots through the front. The display cables run inside, to the rear.
    for bv in S.VENTS_B:
        s = s - round_slot(-52, 52, bv - 1.2, bv + 1.2, -S.WALL - 1, 1)
    # Tongues with a ridge on each side. They go up into the thick front part of the slab
    # walls; the ridge clicks into a groove. Each tongue stands on the front skin of the chin,
    # and above the chin its lower end rises at 45 degrees, so it prints without supports.
    def tongue(sg):
        tin, tout = A_THICK - S.CLR - 2.0, A_THICK - S.CLR
        t = box(sg * tin, sg * tout, S.TONGUE_B[0], S.TONGUE_B[1], -2.4, S.STEP_C + 0.5)
        root = box(sg * (tout - 0.2), sg * (A_IN + 0.1), S.TONGUE_B[0], S.CHIN_B, -2.4, S.STEP_C + 0.5)
        rb = S.RIDGE_B
        pts = [(tout, rb), (tout + 0.9, rb), (tout + 0.9, rb + 0.5), (tout, rb + 2.0)]
        ridge = prism_c([(sg * u, v) for u, v in pts], S.STEP_C + 0.5, -2.8)
        ridge = ridge & prism_b([(-12.0, sg * tout), (-13.0, sg * (tout + 1.0)), (S.STEP_C, sg * (tout + 1.0)),
                                 (S.STEP_C, sg * tout)], rb - 1, rb + 3)
        top = S.TONGUE_B[1]
        keep = prism_a([(S.TONGUE_B[0] - 1, -2.0), (S.CHIN_B, -2.0), (S.CHIN_B, -2.6), (top + 1, -2.6 - (top + 1 - S.CHIN_B)),
                        (top + 1, S.STEP_C), (S.TONGUE_B[0] - 1, S.STEP_C)], -A_OUT - 1, A_OUT + 1)
        return ((t + ridge) & keep) + root

    for t in both_sides(tongue):
        s = s + t
    # Slots in the floor for the hooks of the foot.
    for y in S.FOOT_HOOKS_Y:
        for sg in (1, -1):
            x0, x1 = S.FOOT_HOOK_X - 0.3, S.FOOT_HOOK_X + 1.45
            xm = (x0 + x1) / 2
            pts = [(x0, y - 4.3), (xm, y - 4.3 - 0.9), (x1, y - 4.3), (x1, y + 4.3), (xm, y + 4.3 + 0.9), (x0, y + 4.3)]
            sl = prism_c([(sg * x, yy) for x, yy in pts], S.FOOT_H - 1, S.FOOT_H + S.WALL + 1)
            s = s - LOC.inverse() * sl
    return s


# ---------------------------------------------------------------- back plate and foot


def back():
    """The rear wall: the drive on its front, the fan and the Pi on its rear."""
    c0, c1 = S.PLATE_C
    top = S.H - S.WALL + S.GROOVE - S.CLR
    r = S.R_TOP - S.WALL + S.GROOVE - S.CLR
    s = round_top(box(-A_PLATE, A_PLATE, -20, top, c0, c1), top, r)
    s = s - world_below(S.FOOT_H + S.WALL + S.CLR)
    # Notches in the bottom edge for the display cables, and in the top edge for the drive cable.
    for a0, a1 in S.CABLE_NOTCHES:
        s = s - box(a0, a1, -30, S.CABLE_NOTCH_B, c0 - 1, c1 + 1)
    a0, a1, bn = S.SSD_NOTCH
    s = s - box(a0, a1, bn, top + 1, c0 - 1, c1 + 1)
    # SSD: four M3 screws through the plate, from the rear.
    for a, b in S.SSD_HOLES:
        s = s - cyl_c(a, b, 1.7, c0 - 1, c1 + 1)
    # Bosses for the Pi: M3 heat-set inserts. The male ends of the Pi standoffs go in.
    pi_c = c0 - S.PI_BOSS_H
    for a, b in S.PI_HOLES:
        s = s + cyl_c(a, b, S.PI_BOSS_R, c0 + 0.01, pi_c)
        s = s - cyl_c(a, b, S.INSERT_D / 2, pi_c - 0.01, pi_c + 5.5)
    # Fan: four corner guides, and a boss under each corner hole. An M3 screw goes through the
    # fan corner into a blind pilot hole in the boss. The gap under the fan lets the air in.
    fa, fb = S.FAN_A, S.FAN_B
    hf = S.FAN_FEET
    for sa in (1, -1):
        for sb in (1, -1):
            s = s + box(fa + sa * 15.35, fa + sa * 16.95, fb + sb * 9, fb + sb * 16.95, c0 - hf - 5, c0 + 0.01)
            s = s + box(fa + sa * 9, fa + sa * 16.95, fb + sb * 15.35, fb + sb * 16.95, c0 - hf - 5, c0 + 0.01)
            ha, hb = fa + sa * S.FAN_HOLE, fb + sb * S.FAN_HOLE
            s = s + cyl_c(ha, hb, 3.0, c0 + 0.01, c0 - hf)
            s = s - cyl_c(ha, hb, S.FAN_PILOT_R, c0 - hf - 0.01, c0 + 1.5)
    # Two holes for each twist tie.
    for a, b, half in S.TIES:
        for sg in (1, -1):
            s = s - cyl_c(a + sg * half, b, S.TIE_HOLE_R, c0 - 1, c1 + 1)
    return s


def foot_world():
    """The foot, in world coordinates (it prints in this position)."""
    x = S.W / 2 - S.FOOT_IN
    y1 = S.D / math.cos(S.TILT) + S.FOOT_REAR
    f = Pos(-x, S.FOOT_IN, 0) * Box(2 * x, y1 - S.FOOT_IN, S.FOOT_H, align=MIN)
    f = fillet(f.edges().filter_by(Axis.Z), 8)
    f = chamfer(f.edges().filter_by(lambda e: abs(e.center().Z - S.FOOT_H) < 0.01), 1.0)
    z_floor = S.FOOT_H + S.WALL          # top of the chin floor
    for y in S.FOOT_HOOKS_Y:
        for sg in (1, -1):
            hx = S.FOOT_HOOK_X
            f = f - (Pos(sg * (hx - 1.5 + 2.0), y, 1.5) * Box(4.0, 10.4, S.FOOT_H, align=(Align.CENTER, Align.CENTER, Align.MIN)))
            arm = Pos(sg * (hx + 0.6), y, 1.5) * Box(1.2, 8, z_floor + 5.5 - 1.5, align=(Align.CENTER, Align.CENTER, Align.MIN))
            # Barb: lead-in ramp at the top, a gable under it that sits on the chin floor. The
            # catch face under the barb is 41 degrees from the bed, so it prints without supports.
            u0 = hx + 1.2
            zt = z_floor + 5.5
            zf = z_floor + 0.1
            ramp = prism_b([(zf, sg * u0), (zf + 0.7, sg * (u0 + 0.8)), (zt - 1.0, sg * (u0 + 0.8)), (zt, sg * u0)],
                           y - 4, y + 4)
            gab = prism_a([(y - 4, zt), (y + 4, zt), (y + 4, zf + 4.0), (y, zf), (y - 4, zf + 4.0)],
                          sg * (u0 - 0.2), sg * (u0 + 1.0))
            f = f + arm + (ramp & gab)
    return f


# ---------------------------------------------------------------- hardware
# Each builder returns (parts, zones). Parts: list of (name, shape, material).
# Zones: list of (name, shape). A zone keeps space free for a plug or a cable.


def frame_loc(origin, x, z):
    return Location(Plane(origin=origin, x_dir=x, z_dir=z))


def hexagon(af, h):
    return extrude(RegularPolygon(af / math.sqrt(3), 6), amount=h)


def display():
    m = frame_loc(S.DISP_C, (1, 0, 0), (0, 0, 1))
    p = [
        ("display_lcd", box(-50, 50, -83, 83, -7.35, -0.2), "lcd_metal"),
        ("display_glass", box(-50, 50, -83, 83, -0.2, 0), "black_glass"),
        ("display_board", box(-58.1, 58.1, -82.55, 82.55, -9.35, -7.35), "pcb_green"),
        ("display_sockets", box(-32, 12, -82.55, -66, -15.35, -9.35), "steel"),
        ("display_plug_hdmi", box(-11, 11, -112.55, -78.55, -20.35, -9.35), "black_plastic"),
        ("display_plug_usb", box(-26.5, -14.5, -110.55, -78.55, -17.35, -9.35), "black_plastic"),
    ]
    for i, (x, y) in enumerate(S.EARS):
        p.append((f"display_screw{i}", cyl_c(x, y, 2.75, -9.35, -11.35), "steel"))
    z = [("display_plugs", box(-34, 14, -122.55, -82.6, -20.85, -9.4))]
    return [(n, m * s, mat) for n, s, mat in p], [(n, m * s) for n, s in z]


def ssd():
    """The drive and its USB adapter. Local x: up (b), local y: to the left (-a), local z: to
    the front (c). The drive bottom with the M3 holes lies on the front of the plate."""
    m = frame_loc(S.SSD_O, (0, 1, 0), (0, 0, 1))
    u = -S.SSD_USB_A
    p = [
        ("ssd_drive", box(-50.2, 50.2, -34.9, 34.9, 0, 6.8), "alu_dark"),
        ("ssd_label", box(-40, 30, -25, 25, 6.8, 6.9), "label_black"),
        ("ssd_adapter", box(50.2, 86.3, -33, 33, 2.2, 3.8), "pcb_green"),
        ("ssd_usb", box(78, 86.3, u - 6.25, u + 6.25, 3.8, 7.0), "steel"),
    ]
    parts = [(n, m * s, mat) for n, s, mat in p]
    for i, (a, b) in enumerate(S.SSD_HOLES):
        parts.append((f"ssd_screw{i}", cyl_c(a, b, 2.75, S.PLATE_C[0], S.PLATE_C[0] - 2.0), "steel"))
    return parts, []


def pi_stack():
    """Pi 5 and Amp2, from the vendor models (case/vendor). Local x: toward the USB and Ethernet
    ports (up, +b). Local y: toward the GPIO header (right, +a). Local z: away from the plate
    (to the rear). The bosses of the back plate take the place of the bottom spacers."""
    m = frame_loc((S.PI_A, S.PI_B, S.PLATE_C[0]), (0, 1, 0), (0, 0, -1))
    s = S.PI_BOSS_H
    b = s + 1.5                     # top of the Pi board
    a = b + 16                      # bottom of the Amp2 board
    holes = [(-39, -24.5), (19, -24.5), (-39, 24.5), (19, 24.5)]
    up = (Align.CENTER, Align.CENTER, Align.MIN)
    p = []
    for i, (x, y) in enumerate(holes):
        p.append((f"pi_standoff{i}", Pos(x, y, b) * hexagon(5, 16), "brass"))
        p.append((f"pi_screw{i}", cyl_c(x, y, 2.75, a + 1.5, a + 3.5), "steel"))
    p += [
        ("pi_board", box(-42.5, 42.5, -28, 28, s, b), "pcb_green"),
        ("pi_ethernet", box(23.5, 44.5, -25.8, -9.8, b, b + 13.5), "steel"),
        ("pi_usb3", box(27, 44.5, -5.5, 7.7, b, b + 15.6), "steel"),
        ("pi_usb2", box(27, 44.5, 12.4, 25.6, b, b + 15.6), "steel"),
        ("pi_header", box(-35.4, 15.4, 21.9, 27, b, b + 8.5), "black_plastic"),
        ("pi_usbc", box(-35.8, -26.8, -29.2, -21.8, b, b + 3.2), "steel"),
        ("pi_hdmi0", box(-20.3, -13.1, -29.2, -21.6, b, b + 3), "steel"),
        ("pi_hdmi1", box(-6.9, 0.3, -29.2, -21.6, b, b + 3), "steel"),
        ("pi_soc", box(-20.5, -3.5, -6, 11, b, b + 2.2), "nickel"),
        ("pi_heatsink", box(-20, -4, -5.5, 10.5, b + 2.2, b + 10.2), "alu_dark"),
        ("pi_rp1", box(4.5, 16.5, -4, 8, b, b + 1.2), "black_plastic"),
        ("pi_fan_socket", box(22.8, 25.7, 21, 27, b, b + 3.6), "nickel"),
        ("amp_board", box(-42.5, 22.5, -28, 28, a, a + 1.5), "pcb_blue"),
        ("amp_terminals", box(-15.2, 15.1, -27.7, -17.3, a + 1.5, a + 15.4), "terminal_green"),
        ("amp_jack", box(-25, -15.7, -30, -15.8, a + 1.5, a + 12.4), "black_plastic"),
        ("amp_coil1", box(-15.5, -5.5, -12.6, -3.6, a + 1.5, a + 10.4), "alu_dark"),
        ("amp_coil2", box(1, 11, -12.6, -3.6, a + 1.5, a + 10.4), "alu_dark"),
        ("amp_header", box(-35.4, 15.4, 21.9, 27, a - 7.5, a), "black_plastic"),
    ]
    for i, (x, y) in enumerate(((-20.9, -0.3), (-20.9, 12.6), (16.9, 6.3), (16.9, 15.2))):
        p.append((f"amp_cap{i}", Pos(x, y, a + 1.5) * Cylinder(4.2, 12.4, align=up), "black_plastic"))
    p += [
        # Plugs: USB-C power and the display HDMI (left edge), the drive (USB 3), the touch
        # and the printer (USB 2) at the top edge, the fan, and the right-angle Amp2 power plug.
        ("plug_pi_power", box(-37.3, -25.3, -57.3, -29.3, b - 1.65, b + 4.85), "black_plastic"),
        ("plug_pi_hdmi", box(-22.7, -10.7, -49.3, -29.3, b - 1.5, b + 4.5), "black_plastic"),
        ("plug_ssd_usb", box(44.6, 74.6, -6.4, 8.6, b + 0.8, b + 7.8), "black_plastic"),
        ("plug_touch_usb", box(44.6, 74.6, 11.5, 26.5, b + 8, b + 15), "black_plastic"),
        ("plug_printer_usb", box(44.6, 74.6, 11.5, 26.5, b + 0.8, b + 7.8), "black_plastic"),
        ("plug_fan", box(23.0, 25.5, 21.5, 26.5, b + 3.6, b + 7), "nickel"),
        ("plug_amp_power", box(-25.4, -15.3, -42, -30.1, a + 2, a + 12), "black_plastic"),
    ]
    z = [
        ("pi_port_plugs", box(44.6, 80, -28, 28, b, b + 16.5)),
        ("pi_side_plugs", box(-38, 2, -60, -29.3, b - 2, b + 6)),
        ("amp_side_plugs", box(-26, 16, -45, -28.1, a + 1.5, a + 16)),
    ]
    return [(n, m * sh, mat) for n, sh, mat in p], [(n, m * sh) for n, sh in z]


def fan():
    m = frame_loc((S.FAN_A, S.FAN_B, S.PLATE_C[0] - S.FAN_FEET), (1, 0, 0), (0, 0, -1))
    body = box(-15, 15, -15, 15, 0, 10) - Pos(0, 0, -1) * Cylinder(14, 12, align=(Align.CENTER, Align.CENTER, Align.MIN))
    p = [("fan_frame", body, "black_plastic"),
         ("fan_hub", Pos(0, 0, 0.5) * Cylinder(6.3, 9, align=(Align.CENTER, Align.CENTER, Align.MIN)), "black_plastic")]
    for i, (x, y) in enumerate((x * S.FAN_HOLE, y * S.FAN_HOLE) for x in (-1, 1) for y in (-1, 1)):
        p.append((f"fan_screw{i}", cyl_c(x, y, 2.75, 10, 12), "steel"))
    for i in range(7):
        bl = Rot(0, 0, i * 360 / 7) * Pos(9.75, 0, 5) * Rot(35, 0, 0) * box(-3.75, 3.75, -3.2, 3.2, -0.4, 0.4)
        p.append((f"fan_blade{i}", bl, "black_plastic"))
    return [(n, m * s, mat) for n, s, mat in p], []


# ---------------------------------------------------------------- cables


def rounded_path(points, k=0.45, cut=15.0):
    """The polyline with a tangent arc at each bend. The arc starts and ends at a distance d from
    the corner: 45 % of the shorter segment, max. 15 mm. The arc stays inside the corner."""
    pts = [Vector(*p) for p in points]
    edges, start = [], pts[0]
    for a, b, c in zip(pts, pts[1:], pts[2:]):
        u, w = b - a, c - b
        if u.get_angle(w) < 0.5:          # degrees: no bend
            continue
        d = min(k * u.length, k * w.length, cut)
        p_in, p_out = b - u.normalized() * d, b + w.normalized() * d
        if (p_in - start).length > 1e-3:
            edges.append(Line(start, p_in))
        edges.append(TangentArc(p_in, p_out, tangent=u))
        start = p_out
    edges.append(Line(start, pts[-1]))
    return Wire(edges)


def tube(points, r):
    """A cable: a circle of radius r swept along the polyline, with a smooth bend at each corner."""
    path = rounded_path(points)
    start = Vector(*points[1]) - Vector(*points[0])
    t = sweep(Plane(origin=points[0], z_dir=start) * Circle(r), path=path)
    # A failed sweep can make a bad solid. Stop the build, so the fit check is not blind.
    expect = math.pi * r * r * path.length
    assert t.is_valid and len(t.solids()) == 1 and abs(t.volume - expect) < 0.02 * expect, \
        f"tube sweep failed: {points[0]}"
    return t


def arc_bc(a, b0, c0, r, n=6):
    """A quarter bend: from (a, b0, c0) along +b, to the rear (-c), with radius r."""
    return [(a, b0 + r * math.sin(t), c0 - r + r * math.cos(t))
            for t in (i * math.pi / 2 / (n - 1) for i in range(n))]


def pi_point(x, y, z):
    """A point in the frame of the Pi (see pi_stack), in slab coordinates."""
    return (S.PI_A + y, S.PI_B + x, S.PLATE_C[0] - z)


# The drive plug: micro-B SuperSpeed, straight. Its socket face is at b = SSD_PLUG_B.
SSD_PLUG_B = S.SSD_O[1] + 86.3
SSD_PLUG_C = S.SSD_O[2] + 5.4
MICRO_B_LEN = 22.0                   # from the socket face to the end of the strain relief
PB = S.PI_BOSS_H + 1.5               # top of the Pi board, in the frame of the Pi


def cable_paths():
    """The centre lines of the cables: the two display cables (display plug to Pi plug), the
    drive cable (micro-B plug to the USB 3 plug), and the fan cable (fan to the Pi fan plug)."""
    hdmi = [(0, 19.05, -15.85), (0, 14.5, -17), (2, 13.5, -23), (8, 14.5, -30.25), (14, 16, -34.5),
            (6, 24, -35.5), (-30, 34, -36), (-52, 45, -36), (-55, 60, -36), (-55, 80, -36), (-57, 95, -40),
            (-58, 104, -47), (-56, 110, -53), pi_point(-16.7, -49.3, PB + 1.5)]
    usb = [(-20.5, 21.05, -14.35), (-20.5, 16.5, -15), (-19, 14.5, -22), (-15, 15, -30.25), (-13, 17, -34),
           (-12, 22, -44), (0, 34, -46), (40, 45, -42), (52, 60, -37), (52, 100, -36.5), (52, 170, -37),
           (50, 195, -45), (38, 207, -56), (26, 212, -62), (18, 208, -63.5), pi_point(74.6, 19, PB + 11.5)]
    a = S.SSD_USB_A
    b0 = SSD_PLUG_B + MICRO_B_LEN
    end = pi_point(74.6, 1.1, PB + 4.3)
    ssd = arc_bc(a, b0, SSD_PLUG_C, 11.0) + [
        (a + 4, 215, -42), (-7, 214, -51), (end[0], 209, end[2]), end]
    fan = [pi_point(*p) for p in ((0, 15.2, S.FAN_FEET + 5), (6, 30, 13), (16, 30.5, 20), (21, 30.5, 27),
                                  (24.25, 27, 30), (24.25, 24, PB + 7.1))]
    return {"hdmi": hdmi, "usb": usb, "ssd": ssd, "fan": fan}


def stub_paths():
    """Short pieces of the cables that you bring: power, printer, Amp2 power, speakers."""
    a = PB + 16                      # bottom of the Amp2 board
    p = {
        "stub_power": [pi_point(-31.3, -57.3, PB + 1.6), pi_point(-31.3, -60, PB + 3), pi_point(-31.3, -62, PB + 25),
                       pi_point(-31.3, -62, PB + 60)],
        "stub_printer": [pi_point(74.6, 19, PB + 4.3), pi_point(78, 19, PB + 4.3), pi_point(83, 12, PB + 8),
                         pi_point(84, 8, PB + 23), pi_point(67, 8, PB + 43), pi_point(-8, 8, PB + 48)],
        "stub_amp_power": [pi_point(-20.35, -36, a + 12), pi_point(-20.35, -36, a + 40)],
    }
    for i, x in enumerate((-2.6, 2.4, 7.4, 12.4)):
        p[f"stub_speaker{i}"] = [pi_point(x, -27.7, a + 8), pi_point(x, -34, a + 9), pi_point(x, -38, a + 16),
                                 pi_point(x, -40, a + 40)]
    return p


def cable_length(points):
    return sum(math.dist(p, q) for p, q in zip(points, points[1:]))


def cables():
    """The cables, the drive plug, and the twist ties on the rear face of the back plate."""
    paths = cable_paths()
    out = [("cable_hdmi", tube(paths["hdmi"], 2.0), "black_plastic"),
           ("cable_usb", tube(paths["usb"], 1.75), "black_plastic"),
           ("ssd_cable", tube(paths["ssd"], 2.25), "black_plastic"),
           ("fan_cable", tube(paths["fan"], 0.8), "black_plastic")]
    for name, pts in stub_paths().items():
        r = 1.0 if "speaker" in name else 2.0
        out.append((name, tube(pts, r), "black_plastic"))
    # The micro-B plug: metal, overmould, and strain relief.
    a, b, c = S.SSD_USB_A, SSD_PLUG_B, SSD_PLUG_C
    out += [("ssd_cable_metal", box(a - 6.1, a + 6.1, b, b + 1.7, c - 0.9, c + 0.9), "steel"),
            ("ssd_cable_plug", box(a - 10.5, a + 10.5, b + 1.7, b + 16, c - 3.5, c + 3.5), "black_plastic"),
            ("ssd_cable_relief", Location(Plane(origin=(a, b + 16, c), z_dir=(0, 1, 0)))
             * Cone(3.5, 2.25, MICRO_B_LEN - 16, align=(Align.CENTER, Align.CENTER, Align.MIN)), "black_plastic")]
    for i, (ta, tb, half) in enumerate(S.TIES):
        ring = scale(Torus(5.0, 0.8), by=(half / 5.0, 1, 1))
        out.append((f"tie{i}", Location(Plane(origin=(ta, tb, S.PLATE_C[0] - 5.0), x_dir=(1, 0, 0), z_dir=(0, 1, 0)))
                    * ring, "pla_orange"))
    return out


def screen_corners():
    """Corners of the visible screen, in the world: bottom-left, bottom-right, top-right, top-left."""
    a, b, c = S.DISP_C
    w, h = S.SCREEN_WH
    pts = [(a - w / 2, b - h / 2), (a + w / 2, b - h / 2), (a + w / 2, b + h / 2), (a - w / 2, b + h / 2)]
    out = []
    for x, y in pts:
        p = LOC * Pos(x, y, c + 0.05)
        out.append([p.position.X, p.position.Y, p.position.Z])
    return out
