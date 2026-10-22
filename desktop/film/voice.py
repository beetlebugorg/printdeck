# The announcer of the film (film.sh): Piper speaks each line at its time, and the music goes
# softer under the voice. The lines follow the cards of FilmTitles.qml.
#
#   uv run --no-project --with piper-tts --with numpy python desktop/film/voice.py \
#       VOICE.onnx MUSIC.wav OUT.wav
import sys
import wave

import numpy as np
from piper import PiperVoice, SynthesisConfig

# The time of each line in seconds, and its words.
LINES = [
    (0.9, "Printdeck. A touch screen for Klipper."),
    (8.5, "Built for the TAZ Workhorse..."),
    (13.0, "and printed on the printer it mounts on."),
    (18.6, "A seven inch touch screen."),
    (25.5, "The printer's own display, redrawn."),
    (34.5, "The Klipper host, rewritten in Go."),
    (39.4, "Send from Cura. Watch it print."),
    (47.5, "One Raspberry Pi five. One Go program."),
    (55.2, "Printdeck."),
]

model, music_path, out_path = sys.argv[1:4]
voice = PiperVoice.load(model)
syn = SynthesisConfig(length_scale=1.08)  # a little slower than normal speech

with wave.open(music_path, "rb") as w:
    rate = w.getframerate()
    music = np.frombuffer(w.readframes(w.getnframes()), "<i2").astype(np.float32).reshape(-1, 2) / 32768

speech = np.zeros(len(music), np.float32)
for at, text in LINES:
    chunks = list(voice.synthesize(text, syn_config=syn))
    x = np.concatenate([c.audio_float_array for c in chunks]).astype(np.float32)
    src = chunks[0].sample_rate
    # To the rate of the music.
    x = np.interp(np.arange(int(len(x) * rate / src)) * src / rate, np.arange(len(x)), x)
    i = int(at * rate)
    end = min(len(speech), i + len(x))
    speech[i:end] += x[: end - i]
    if end - i < len(x):
        print("the line at", at, "s is cut at the end of the music")

# The music is 45 % under the voice: a gain that follows the level of the voice, smoothed.
level = np.convolve(np.abs(speech), np.ones(int(0.25 * rate)) / (0.25 * rate), "same")
duck = 1 - 0.55 * np.clip(level / (level.max() * 0.25), 0, 1)
mix = music * duck[:, None] + 0.9 * speech[:, None] / np.max(np.abs(speech))
mix /= max(1.0, np.max(np.abs(mix)) / 0.95)

with wave.open(out_path, "wb") as w:
    w.setnchannels(2)
    w.setsampwidth(2)
    w.setframerate(rate)
    w.writeframes((mix * 32767).astype("<i2").tobytes())
print("wrote", out_path)
