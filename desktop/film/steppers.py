# The sound of the printer in the film (film.sh): the song of the stepper motors, from the moves
# that the take recorded (take.mp4.motion.csv), under the music and the voice.
#
# A stepper sings at the rate of its full steps: 200 full steps for each turn, so 200 / 32 =
# 6.25 Hz for each mm/s on X and Y (rotation_distance 32 in printer.cfg) and 200 / 6.4 = 31.25 Hz
# for each mm/s on Z. The tone has strong harmonics, and the microsteps add a little hiss. The
# fans of the tool head hum. The printer is heard only in the close-up of the nozzle (CLOSE), and
# softly.
#
#   uv run --no-project --with numpy python desktop/film/steppers.py MOTION.csv SOUNDTRACK.wav OUT.wav
import csv
import sys
import wave

import numpy as np

HZ_PER_MM_S = {"x": 200 / 32, "y": 200 / 32, "z": 200 / 6.4}
# The close-up of the nozzle in the film (the shots of Sim3D.qml), in seconds, and the fade.
CLOSE, FADE = (51.0, 65.0), 1.5
# The moment when the steppers sing alone: the music and the voice go quiet.
SOLO, SOLO_FADE = (52.4, 55.0), 0.4

motion_path, track_path, out_path = sys.argv[1:4]
rows = list(csv.DictReader(open(motion_path)))
fps = float(rows[0]["fps"])
frames = np.array([float(r["frame"]) for r in rows])
pos = {a: np.array([float(r[a]) for r in rows]) for a in "xyz"}

with wave.open(track_path, "rb") as w:
    rate = w.getframerate()
    track = np.frombuffer(w.readframes(w.getnframes()), "<i2").astype(np.float32).reshape(-1, 2) / 32768
n = len(track)
t = np.arange(n) / rate
rng = np.random.default_rng(3)


def smooth(x, k=3):
    return np.convolve(x, np.ones(k) / k, "same")


song = np.zeros(n)
for axis, hz_per in HZ_PER_MM_S.items():
    # The speed of the axis at each frame, in mm/s, then at each sample.
    v = smooth(np.abs(np.gradient(pos[axis], frames / fps)))
    v = np.interp(t, frames / fps, v)
    v[v < 0.5] = 0
    f = v * hz_per
    phase = 2 * np.pi * np.cumsum(f) / rate
    tone = sum(np.sin(k * phase) / k ** 1.1 for k in range(1, 7))
    level = np.clip(v / 20, 0, 1) ** 0.6
    hiss = np.diff(rng.standard_normal(n + 1)) * 0.04
    song += level * (tone + hiss) * (0.6 if axis == "z" else 1.0)

# The resonance of the frame: a soft low-pass.
a = 1 - np.exp(-2 * np.pi * 3500 / rate)
out = np.empty(n)
acc = 0.0
for i in range(n):
    acc += a * (song[i] - acc)
    out[i] = acc
song = out / max(1e-9, np.max(np.abs(out)))

# The fans: noise that is low-passed and steady.
fan = np.convolve(rng.standard_normal(n), np.ones(40) / 40, "same")
fan /= np.max(np.abs(fan))

fade = np.clip(np.minimum(t - CLOSE[0], CLOSE[1] - t) / FADE, 0, 1)
solo = np.clip(np.minimum(t - SOLO[0], SOLO[1] - t) / SOLO_FADE, 0, 1)
printer = fade * (0.05 + 0.3 * solo) * song + fade * 0.012 * fan
mix = track * (1 - 0.8 * solo)[:, None] + printer[:, None]
mix /= max(1.0, np.max(np.abs(mix)) / 0.95)
with wave.open(out_path, "wb") as w:
    w.setnchannels(2)
    w.setsampwidth(2)
    w.setframerate(rate)
    w.writeframes((mix * 32767).astype("<i2").tobytes())
print("wrote", out_path, "from", len(rows), "frames at", fps, "fps")
