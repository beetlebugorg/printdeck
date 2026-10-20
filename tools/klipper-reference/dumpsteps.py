# Writes a random step generation scenario for stepharness.c, as text. Each number is written
# as a hex float, so the C harness and the Go test read the same values. With the argument
# "extruder", the stepper is an extruder with pressure advance.
#
#   python3 dumpsteps.py [extruder] > SCENARIO.txt
import random, sys
extruder = sys.argv[1:] == ["extruder"]
rnd = random.Random(11)
step_dist = 0.0024 if extruder else 0.0125
out = ["setup %s 25 16000000 %d" % (step_dist.hex(), 3 if extruder else 0)]
pt = 0.25
pos = [0., 0., 0.]
last_gen = 0.
for i in range(60):
    r = rnd.random()
    if extruder and r < 0.2:
        out.append("pa %s %s %s" % (float(pt).hex(), rnd.choice([0., rnd.uniform(.02, .08), rnd.uniform(.02, .08)]).hex(),
                                    rnd.choice([0., rnd.uniform(.02, .04)]).hex()))
        continue
    if extruder:
        # As extruder.py queues a move: x is the filament, with axes_r.x of 1 and the sign of a
        # retract in the velocities. y is 1 for a move with pressure advance (an extrude move
        # with xy motion).
        sign = rnd.choice([1., 1., 1., -1.])
        d = [1., rnd.choice([0., 1.]), 0.]
        accel = sign * rnd.uniform(100, 1000)
        cruise_v = sign * rnd.uniform(.2, 6)
    else:
        d = [rnd.uniform(-1, 1) for j in range(3)]
        if rnd.random() < 0.2:
            d[0] = 0.
        l = sum(x * x for x in d) ** .5 or 1.
        d = [x / l for x in d]
        accel = rnd.uniform(500, 3000)
        cruise_v = rnd.uniform(5, 150)
    # Each move starts and ends at rest, so the velocity has no jumps, as in Klipper.
    start_v = 0.
    accel_t = cruise_v / accel
    cruise_t = rnd.choice([0., rnd.uniform(.001, .3)])
    decel_t = cruise_v / accel
    vals = [pt, accel_t, cruise_t, decel_t] + pos + d + [start_v, cruise_v, accel]
    out.append("move " + " ".join(float(v).hex() for v in vals))
    # The end position of the move
    dist = (start_v + .5 * accel * accel_t) * accel_t + cruise_v * cruise_t + (cruise_v - .5 * accel * decel_t) * decel_t
    pos = [p + x * dist for p, x in zip(pos, d)]
    pt += accel_t + cruise_t + decel_t + rnd.choice([0., 0., rnd.uniform(.01, .5)])
    if rnd.random() < 0.4:
        # The flush times only go forward, as in Klipper.
        last_gen = max(last_gen, pt - rnd.uniform(0, .2))
        out.append("gen " + float(last_gen).hex())
out.append("gen " + float(pt + 1.).hex())
if extruder:
    # The smoothing of pressure advance reads the moves after the flush time. Klipper adds
    # these moves before it flushes, so here all moves go first.
    out = ([l for l in out if not l.startswith(("gen", "pa"))]
           + [l for l in out if l.startswith(("gen", "pa"))])
print("\n".join(out))
