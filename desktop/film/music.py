# The music of the film (film.sh): 60 s of synth, made here from sine, saw and square waves and
# noise. The tempo puts the start of bar 9 at 17.4 s, when the display of the Printdeck turns on
# (screenOn in Sim3D.qml): the pad and a soft arpeggio until then, the bass and the kick from bar
# 5 (the assembly), and the full beat from bar 9. A last chord starts at 54.5 s (the end card).
#
#   uv run --no-project --with numpy python desktop/film/music.py OUT.wav
import sys
import wave

import numpy as np

RATE = 44100
LENGTH = 60.0
BEAT = 17.4 / 32  # 8 bars of 4 beats before the display turns on
BAR = 4 * BEAT
N = int(RATE * LENGTH)

# A minor: Am, F, C, G, one bar each. The notes are MIDI numbers.
CHORDS = [[57, 60, 64], [53, 57, 60], [48, 52, 55], [55, 59, 62]]
BASS = [33, 29, 36, 31]


def hz(note):
    return 440.0 * 2 ** ((note - 69) / 12)


def t_of(n):
    return np.arange(n) / RATE


def saw(f, n, detune=0.0):
    t = t_of(n)
    return 2 * ((t * f * (1 + detune)) % 1.0) - 1


def square(f, n):
    return np.sign(np.sin(2 * np.pi * f * t_of(n)))


def lowpass(x, cutoff):
    """A one-pole low-pass filter, with a cutoff in Hz (a number or one value for each sample)."""
    a = 1 - np.exp(-2 * np.pi * np.broadcast_to(cutoff, x.shape) / RATE)
    y = np.empty_like(x)
    acc = 0.0
    for i in range(len(x)):
        acc += a[i] * (x[i] - acc)
        y[i] = acc
    return y


def env(n, attack, release):
    """An envelope of n samples: a linear attack and an exponential release, in seconds."""
    t = t_of(n)
    e = np.minimum(1, t / max(attack, 1e-4))
    return e * np.exp(-np.maximum(0, t - attack) / release)


def put(track, start, x, gain=1.0):
    i = int(start * RATE)
    if i >= len(track):
        return
    x = x[: len(track) - i]
    track[i : i + len(x)] += gain * x


def reverb(x, seconds=1.6, mix=0.25, seed=1):
    """Convolution with a decaying noise burst."""
    rng = np.random.default_rng(seed)
    n = int(seconds * RATE)
    ir = rng.standard_normal(n) * np.exp(-6 * t_of(n) / seconds)
    ir /= np.sqrt(np.sum(ir ** 2))
    size = 1 << int(np.ceil(np.log2(len(x) + n)))
    wet = np.fft.irfft(np.fft.rfft(x, size) * np.fft.rfft(ir, size), size)[: len(x)]
    return (1 - mix) * x + mix * wet


rng = np.random.default_rng(7)
pad, arp, bass, drums = (np.zeros(N) for _ in range(4))
bars = int(np.ceil(LENGTH / BAR))
end_card = 54.5

for b in range(bars):
    start = b * BAR
    if start >= end_card:
        break
    chord, root = CHORDS[b % 4], BASS[b % 4]
    full = b >= 8
    # The pad: two detuned saws for each note, soft and low-passed.
    n = int(BAR * RATE)
    p = sum(saw(hz(note), n, d) for note in chord for d in (-0.004, 0.004))
    put(pad, start, lowpass(p * env(n, 0.4, 3.0), 900 if full else 600), 0.06)
    # The arpeggio: sixteenths over the chord, a filter that opens toward the drop.
    for s in range(16):
        note = chord[s % 3] + (12 if s % 8 >= 4 else 0) + 12
        m = int(BEAT / 4 * RATE)
        cutoff = 1200 + 2600 * min(1, b / 8)
        put(arp, start + s * BEAT / 4, lowpass(square(hz(note), m) * env(m, 0.002, 0.07), cutoff), 0.05)
    if b >= 4:
        # The bass: eighths of the root, with a pump from the kick.
        for e in range(8):
            m = int(BEAT / 2 * RATE)
            x = saw(hz(root + (12 if e % 2 else 0)), m) * env(m, 0.005, 0.18)
            put(bass, start + e * BEAT / 2, lowpass(x, 700), 0.22)
        # The kick on each beat.
        for k in range(4):
            m = int(0.35 * RATE)
            t = t_of(m)
            f = 45 + 80 * np.exp(-t / 0.03)
            put(drums, start + k * BEAT, np.sin(2 * np.pi * np.cumsum(f) / RATE) * np.exp(-t / 0.12), 0.6)
    if full:
        # The snare on 2 and 4, with a short, gated reverb; the hi-hat on the eighths.
        for k in (1, 3):
            m = int(0.25 * RATE)
            t = t_of(m)
            x = rng.standard_normal(m) * np.exp(-t / 0.06) + 0.5 * np.sin(2 * np.pi * 190 * t) * np.exp(-t / 0.05)
            put(drums, start + k * BEAT, x, 0.22)
        for e in range(8):
            m = int(0.05 * RATE)
            x = np.diff(rng.standard_normal(m + 1)) * np.exp(-t_of(m) / 0.012)
            put(drums, start + e * BEAT / 2, x, 0.05 if e % 2 else 0.03)

# The rise before the drop: noise that opens over the last bar before 17.4 s.
m = int(BAR * RATE)
rise = lowpass(rng.standard_normal(m), np.linspace(200, 6000, m)) * np.linspace(0, 1, m) ** 2
put(drums, 17.4 - BAR, rise, 0.25)

# The end card: the first chord, long, and a low boom.
m = int((LENGTH - end_card) * RATE)
last = sum(saw(hz(note), m, d) for note in CHORDS[0] + [45] for d in (-0.004, 0.004))
put(pad, end_card, lowpass(last * env(m, 0.01, 2.2), 1400), 0.08)
t = t_of(m)
put(drums, end_card, np.sin(2 * np.pi * np.cumsum(40 + 60 * np.exp(-t / 0.05)) / RATE) * np.exp(-t / 0.8), 0.6)

# The mix: reverb on the pad and the arpeggio, a stereo delay on the arpeggio, and a fade out.
pad = reverb(pad, 2.5, 0.4)
arp = reverb(arp, 1.2, 0.25, seed=2)
delay = int(BEAT * 0.75 * RATE)
left = pad + bass + drums + arp + 0.35 * np.concatenate([np.zeros(delay), arp[:-delay]])
right = pad + bass + drums + arp + 0.35 * np.concatenate([np.zeros(delay * 2), arp[: -delay * 2]])
mix = np.stack([left, right], axis=1)
mix[-int(1.5 * RATE):] *= np.linspace(1, 0, int(1.5 * RATE))[:, None]
mix = np.tanh(1.2 * mix / np.max(np.abs(mix))) * 0.89

with wave.open(sys.argv[1], "wb") as w:
    w.setnchannels(2)
    w.setsampwidth(2)
    w.setframerate(RATE)
    w.writeframes((mix * 32767).astype("<i2").tobytes())
print("wrote", sys.argv[1], LENGTH, "s at", round(60 / BEAT, 2), "BPM")
