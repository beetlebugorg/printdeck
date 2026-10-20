// Runs a step generation scenario on Klipper's C motion code (trapq.c, itersolve.c,
// kin_cartesian.c, stepcompress.c) and writes the queue_step and set_next_step_dir messages. The
// tests of the Go port of the motion code run the same scenario. dumpsteps.sh builds and runs it.
//
// Input lines (numbers in any strtod form):
//   setup STEP_DIST MAX_ERROR MCU_FREQ AXIS
//   move PRINT_TIME ACCEL_T CRUISE_T DECEL_T SX SY SZ RX RY RZ START_V CRUISE_V ACCEL
//   gen FLUSH_TIME           (update the trapq sentinels, generate steps to FLUSH_TIME, then
//                            flush to its clock, as steppersync.c does)
//   invert 0|1
//   finalize PRINT_TIME
// Output lines: "msg MIN_CLOCK REQ_CLOCK HEXBYTES" for each message, and "pos COMMANDED_POS
// LAST_POSITION" after each gen.
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "list.h"
#include "msgblock.h"
#include "itersolve.h"
#include "stepcompress.h"
#include "trapq.h"

struct stepper_kinematics *cartesian_stepper_alloc(char axis);

static void
drain(struct list_head *q)
{
    while (!list_empty(q)) {
        struct queue_message *qm = list_first_entry(q, struct queue_message, node);
        list_del(&qm->node);
        printf("msg %llu %llu ", (unsigned long long)qm->min_clock
               , (unsigned long long)qm->req_clock);
        for (int i = 0; i < qm->len; i++)
            printf("%02x", qm->msg[i]);
        printf("\n");
        message_free(qm);
    }
}

int
main(void)
{
    char line[1024];
    struct trapq *tq = trapq_alloc();
    struct list_head q;
    list_init(&q);
    struct stepcompress *sc = stepcompress_alloc(&q);
    struct stepper_kinematics *sk = NULL;
    double freq = 0.;
    while (fgets(line, sizeof(line), stdin)) {
        char *p = line, *word = strtok(p, " \n");
        if (!word)
            continue;
        double a[13];
        int n = 0;
        char *t;
        while (n < 13 && (t = strtok(NULL, " \n")))
            a[n++] = strtod(t, NULL);
        if (!strcmp(word, "setup")) {
            freq = a[2];
            sk = cartesian_stepper_alloc('x' + (int)a[3]);
            itersolve_set_trapq(sk, tq, a[0]);
            stepcompress_fill(sc, 3, (uint32_t)a[1], 7, 9);
            stepcompress_set_time(sc, 0., freq);
        } else if (!strcmp(word, "move")) {
            trapq_append(tq, a[0], a[1], a[2], a[3], a[4], a[5], a[6], a[7], a[8]
                         , a[9], a[10], a[11], a[12]);
        } else if (!strcmp(word, "gen")) {
            trapq_check_sentinels(tq);
            int ret = itersolve_generate_steps(sk, sc, a[0]);
            if (!ret)
                ret = stepcompress_flush(sc, (uint64_t)(a[0] * freq));
            drain(&q);
            printf("pos %a %lld ret=%d\n", sk->commanded_pos
                   , (long long)stepcompress_find_past_position(sc, UINT64_MAX), ret);
        } else if (!strcmp(word, "invert")) {
            stepcompress_set_invert_sdir(sc, (uint32_t)a[0]);
        } else if (!strcmp(word, "finalize")) {
            trapq_finalize_moves(tq, a[0], 0.);
        }
    }
    return 0;
}
