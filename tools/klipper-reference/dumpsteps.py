# Writes a random step generation scenario for stepharness.c, as text. Each number is written
# as a hex float, so the C harness and the Go test read the same values.
#
#   python3 dumpsteps.py > SCENARIO.txt
import random
rnd = random.Random(11)
out = ["setup %s 25 16000000 0" % (0.0125).hex()]
pt = 0.25
pos = [0., 0., 0.]
last_gen = 0.
for i in range(60):
    r = rnd.random()
    if r < 0.0:
        out.append("invert %d" % rnd.randint(0, 1))
        continue
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
print("\n".join(out))
