# Runs a list of trapq operations on Klipper's C trapq (klippy/chelper/trapq.c) and writes the
# operations and the results of the extract operations as JSON. The tests of the Go port of trapq
# run the same operations.
#
#   python3 dumptrapq.py OUT.json
import sys, json, random
sys.path.insert(0, '/klipper/klippy')
import chelper

ffi_main, ffi_lib = chelper.get_ffi()
tq = ffi_lib.trapq_alloc()
rnd = random.Random(7)
ops = []
pt = 0.5
for i in range(40):
    r = rnd.random()
    if r < 0.6:
        args = [pt, rnd.choice([0., rnd.uniform(.01, .2)]), rnd.choice([0., rnd.uniform(.01, .5)]),
                rnd.choice([0., rnd.uniform(.01, .2)]), rnd.uniform(-50, 50), rnd.uniform(-50, 50),
                rnd.uniform(0, 10), rnd.uniform(-1, 1), rnd.uniform(-1, 1), rnd.uniform(-1, 1),
                rnd.uniform(0, 20), rnd.uniform(20, 100), rnd.uniform(100, 3000)]
        ffi_lib.trapq_append(tq, *args)
        ops.append(["append", args])
        pt += args[1] + args[2] + args[3] + rnd.choice([0., 0., rnd.uniform(.1, 2.)])
    elif r < 0.8:
        args = [pt - rnd.uniform(0, 1.), pt - rnd.uniform(1, 3)]
        ffi_lib.trapq_finalize_moves(tq, *args)
        ops.append(["finalize", args])
    elif r < 0.85:
        args = [pt - rnd.uniform(0, .5), rnd.uniform(-5, 5), rnd.uniform(-5, 5), rnd.uniform(0, 5)]
        ffi_lib.trapq_set_position(tq, *args)
        ops.append(["set_position", args])
    else:
        args = [rnd.randint(1, 20), pt - rnd.uniform(0, 4), pt + rnd.uniform(-1, 1)]
        data = ffi_main.new('struct pull_move[%d]' % args[0])
        n = ffi_lib.trapq_extract_old(tq, data, *args)
        res = [[m.print_time, m.move_t, m.start_v, m.accel, m.start_x, m.start_y, m.start_z,
                m.x_r, m.y_r, m.z_r] for m in data[0:n]]
        ops.append(["extract", args, res])
with open(sys.argv[1], 'w') as f:
    json.dump(ops, f)
    f.write('\n')
