# Writes the temperature conversions of Klipper's thermistor.py and adc_temperature.py as JSON:
# for each thermistor of temperature_sensors.cfg and each default sensor table, the temperature
# of a list of adc values and the adc value of a list of temperatures. The tests of the Go port
# compare their result with it.
#
#   python3 dumptemps.py OUT.json
import sys, json
sys.path.insert(0, '/klipper/klippy')
import configfile
from extras import thermistor, adc_temperature

class Config:
    # The options of a sensor section: the defaults of the getters.
    def getfloat(self, name, default=None, **kw):
        return default
    def get_name(self):
        return "test"

ADCS = [i / 64. for i in range(65)] + [.00001, .99999, .3333]
TEMPS = [-300., -50., 0., 20., 25., 100., 150., 210., 260., 300., 400., 600.]

def convert(conv):
    temps = [conv.calc_temp(a) for a in ADCS]
    adcs = []
    for t in TEMPS:
        try:
            adcs.append(conv.calc_adc(t))
        except Exception as e:
            adcs.append(None)
    return {"temps": temps, "adcs": adcs}

r = configfile.ConfigFileReader()
path = '/klipper/klippy/extras/temperature_sensors.cfg'
fc = r.build_fileconfig(r.read_config_file(path), path)
out = {"adcs": ADCS, "temps": TEMPS, "sensors": {}}
for s in fc.sections():
    if not s.startswith("thermistor "):
        continue
    g = lambda o: float(fc.get(s, o))
    t = thermistor.Thermistor(4700., 0.)
    if fc.has_option(s, "beta"):
        t.setup_coefficients_beta(g("temperature1"), g("resistance1"), g("beta"))
    else:
        p = sorted([(g("temperature%d" % i), g("resistance%d" % i)) for i in (1, 2, 3)])
        t.setup_coefficients(p[0][0], p[0][1], p[1][0], p[1][1], p[2][0], p[2][1])
    out["sensors"][s] = convert(t)
for name, params in adc_temperature.DefaultVoltageSensors:
    out["sensors"][name] = convert(adc_temperature.LinearVoltage(Config(), params))
for name, params in adc_temperature.DefaultResistanceSensors:
    out["sensors"][name] = convert(adc_temperature.LinearResistance(Config(), params))
with open(sys.argv[1], 'w') as f:
    json.dump(out, f)
    f.write('\n')
