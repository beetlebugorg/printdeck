# Renders the cases of a file with Jinja2, with the environment of Klipper's gcode_macro.py, and
# writes the output (or the error) of each case as JSON. The tests of the Go template engine
# compare their output with it.
#
# A case starts with a line "== CONTEXT" where CONTEXT is a JSON object, and the template is the
# lines after it.
#
#   python3 dumpjinja.py CASES.txt OUT.json
import sys, json
import jinja2

env = jinja2.Environment('{%', '%}', '{', '}')
cases = []
for part in open(sys.argv[1]).read().split('\n== ')[1:]:
    ctx_line, _, template = part.partition('\n')
    ctx = json.loads(ctx_line)
    out = {"template": template, "context": ctx_line}
    try:
        out["output"] = str(env.from_string(template).render(ctx))
    except jinja2.exceptions.TemplateSyntaxError as e:
        out["syntax_error"] = e.message
    except Exception as e:
        out["error"] = type(e).__name__ + ": " + str(e)
    cases.append(out)
with open(sys.argv[2], 'w') as f:
    json.dump(cases, f, indent=1)
    f.write('\n')
