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

# The time of each line in seconds, and its words: a text, or a list of texts and pauses in
# seconds, for the timing of a joke.
LINES = [
    (0.3, ["We buy a 3D printer.", 0.2, "We print an articulated dragon.", 0.2, "Then twelve more.", 0.25,
           "Then... parts for the printer."]),
    (9.2, ["Printdeck is that part.", 0.3, "A touch screen for the Taz Workhorse."]),
    (14.3, ["Printed on the printer it's for.", 0.25, "Naturally."]),
    (18.2, ["Twenty seven megabytes.", 0.25, "Smaller than most cat videos."]),
    (23.4, ["It tracks every roll of filament.", 0.2, "What it cost.", 0.2, "Which prints ate it.", 0.45,
            "It does not judge.", 0.35, "Much."]),
    (34.4, ["Your old LCD?", 0.3, "It's the second display now.", 0.35, "It's taking it well."]),
    (40.8, ["And on your computer?", 0.25, "It's right there in Cura, too."]),
    (45.2, ["It picks the loaded roll...", 0.2, "warns you when it's too short.", 0.25,
            "Before the print. Not after."]),
    (55.0, ["Hear that?", 0.3, "I love the sound of stepper motors at night."]),
    (60.5, ["The Klipper host, in Go.", 0.2, "No Python.", 0.15, "No pip install at midnight."]),
    (66.8, ["One Pi.", 0.15, "One program.", 0.3, "And a touch screen the printer never asked for."]),
    (73.1, ["Printdeck.", 0.4, "Print the next thing for your printer."]),
]

model, voices, voice, music_path, out_path = sys.argv[1:6]
kokoro = Kokoro(model, voices)

with wave.open(music_path, "rb") as w:
    rate = w.getframerate()
    music = np.frombuffer(w.readframes(w.getnframes()), "<i2").astype(np.float32).reshape(-1, 2) / 32768

speech = np.zeros(len(music), np.float32)
for at, text in LINES:
    parts, src = [], 24000
    for piece in text if isinstance(text, list) else [text]:
        if isinstance(piece, (int, float)):
            parts.append(np.zeros(int(piece * src), np.float32))
            continue
        y, src = kokoro.create(piece, voice=voice, speed=0.95, lang="en-us")
        parts.append(np.asarray(y, np.float32))
    x = np.concatenate(parts)
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
