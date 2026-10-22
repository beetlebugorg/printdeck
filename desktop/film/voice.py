# The announcer of the film (film.sh): Kokoro (kokoro-onnx) speaks each line at its time, and the
# music goes softer under the voice. The lines are a joke about a touch screen that a printer
# does not need, timed to the shots of Sim3D.qml.
#
#   uv run --no-project --with kokoro-onnx --with numpy python desktop/film/voice.py \
#       kokoro-v1.0.onnx voices-v1.0.bin VOICE MUSIC.wav OUT.wav
#
# VOICE is a voice of Kokoro, for example am_michael.
import sys
import wave

import numpy as np
from kokoro_onnx import Kokoro

# The time of each line in seconds, and its words.
LINES = [
    (0.5, "3D printer people are a peculiar type."),
    (4.0, "They buy 3D printers... to print things for their 3D printers."),
    (8.8, "Printdeck is no different. A touch screen for the Taz Workhorse. Let's take a look. Anyway."),
    (15.0, "Its case? Printed on the very printer it's for."),
    (18.1, "It boots a system smaller than most cat videos. Twenty seven megabytes."),
    (23.4, "It knows your rolls of filament. What they cost. Which prints ate them. And which ones you've been ignoring."),
    (34.4, "Your old LCD? Still works. It's the second display now. It's taking it well."),
    (40.6, "Slice in Cura, and the whole deck is right there, in its own tab."),
    (45.4, "Cura switches to the roll that's loaded... and warns you when it's too short. Rude. But helpful."),
    (55.0, "Hear that? Your stepper motors. Enjoy them. Your software isn't getting in their way."),
    (60.7, "Underneath, the Klipper host, rewritten in Go. No Python. No dependencies."),
    (66.4, "One Raspberry Pi. One program. And a printer with a touch screen it never asked for."),
    (72.8, "Printdeck. Print the next thing for your printer."),
]

model, voices, voice, music_path, out_path = sys.argv[1:6]
kokoro = Kokoro(model, voices)

with wave.open(music_path, "rb") as w:
    rate = w.getframerate()
    music = np.frombuffer(w.readframes(w.getnframes()), "<i2").astype(np.float32).reshape(-1, 2) / 32768

speech = np.zeros(len(music), np.float32)
for at, text in LINES:
    x, src = kokoro.create(text, voice=voice, speed=0.95, lang="en-us")
    x = np.asarray(x, np.float32)
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
