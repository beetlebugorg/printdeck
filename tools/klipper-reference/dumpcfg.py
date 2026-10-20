# Writes what Klipper's config reader (klippy/configfile.py, on Python's configparser) reads from
# config files, as JSON: [[section, [[option, value], ...]], ...]. The tests of the Go port of
# configfile compare their result with it.
#
#   python3 dumpcfg.py FILE.cfg OUT.json [FILE.cfg OUT.json ...]
import sys, json
sys.path.insert(0, '/klipper/klippy')
import configfile

r = configfile.ConfigFileReader()
for path, out in zip(sys.argv[1::2], sys.argv[2::2]):
    data = r.read_config_file(path)
    fc = r.build_fileconfig_with_includes(data, path)
    res = [[s, [[o, fc.get(s, o)] for o in fc.options(s)]] for s in fc.sections()]
    with open(out, 'w') as f:
        json.dump(res, f, indent=1)
        f.write('\n')
