"""Convert the official Raspberry Pi 5 STEP file to an STL mesh. Run with: freecadcmd convert_pi5.py"""
import os
import sys, time
import Part, MeshPart
here = os.path.dirname(os.path.abspath(__file__))
src = os.path.join(here, "src/rpi5/rpi-5b_no_graphics.step")
dst = os.path.join(here, "raspberry_pi_5.stl")
t = time.time()
shape = Part.Shape()
shape.read(src)
b = shape.BoundBox
print("read %.0fs bbox min=(%.2f, %.2f, %.2f) max=(%.2f, %.2f, %.2f) size=(%.2f, %.2f, %.2f)" % (
    time.time() - t, b.XMin, b.YMin, b.ZMin, b.XMax, b.YMax, b.ZMax, b.XLength, b.YLength, b.ZLength), flush=True)
mesh = MeshPart.meshFromShape(Shape=shape, LinearDeflection=0.15, AngularDeflection=0.6, Relative=False)
print("mesh %.0fs facets %d" % (time.time() - t, mesh.CountFacets), flush=True)
mesh.write(dst)
print("done", flush=True)
