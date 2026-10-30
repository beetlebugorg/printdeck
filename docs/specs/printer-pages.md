# Printer pages of a project

A project gets a page for each printer that printed its parts. The page shows a 3D model of the
printer that the viewer can turn, with the parts of the project on its bed.

## Addresses

- `/projects/<project slug>/printers/<printer slug>`: the page of a printer in a project.
- The printer slug comes from the name of the printer, with the rules of names.go. Old slugs go
  to the new slug, as for the projects.
- The printer chips on the project page link to these pages.
- The project page gets a "Printers" section: one line for each printer, with its model, its
  process (FDM or resin), and the count of its prints in the project.

The pages have the access rules of the project pages: passkey, members only.

## The page

1. The 3D view, at the top. The viewer turns it with a finger or the mouse, and zooms it.
   - The printer model, with the deck pod where the printer has one.
   - The selected print of the project on the bed, at its place on the bed.
2. The header: the name of the printer, its model, its process, and the deck that drives it.
3. The numbers for the project: the prints, the complete prints, the print time, and the
   material (filament in g or m for FDM; resin in ml when the file gives it).
4. The live state when the printer prints for this project now, and the time of the last report.
5. The prints of the project on this printer, newest first, as tiles. A tap on a tile puts that
   print on the bed of the 3D view. A second tap opens the print page.

## The 3D view

- three.js, an exact version from cdn.jsdelivr.net (an import map), with OrbitControls.
- One GLB file for each printer model, in the static files of the web pages
  (`deploy/web/assets/models/<model>.glb`). The size target is 3 MB or less for each file.
- Each GLB has a node named `bed`. Its transform maps the coordinates of the printer (mm, X to
  the right, Y to the back, Z up, as in the G-code) to the scene. The part goes under this node.
- A printer with no GLB of its model gets a plain model of its process: a box frame and a bed
  for FDM, a box with a vat and a plate for resin.

### The printer models

- **TAZ Workhorse.** From the meshes of `lulzbot/workhorse.py` (the LulzBot source files) and the
  pod of `lulzbot/pod.py`. A new script, `lulzbot/printer_glb.py`, joins the meshes by color,
  reduces them to about 150,000 triangles, and writes the GLB with the `bed` node. The LulzBot
  files are GPLv3 or later and CC BY-SA 4.0. The page shows the credit and a link to the source,
  and the GLB has the same licenses.
- **Peopoly Phenom.** No CAD file of the Phenom is in the repo. The script builds a simple model
  from the size of the printer and its build volume (276 x 155 x 400 mm): the case, the UV screen,
  the vat, the plate and the Z column. The model is marked as approximate in the code.

### The parts on the bed

- **FDM (phase 2).** The deck uploads `path.bin` with each print: the extrusion moves of the
  G-code as line strips, with 16-bit coordinates in units of 0.05 mm and a byte for the feature
  (wall, infill, support). The deck keeps at most 300,000 segments: it drops the infill first,
  then joins short segments. The page draws the strips as lines in the color of the filament.
  A print that stopped early shows the moves up to Record.Pos in color and the rest in gray.
- **Resin (phase 3).** The deck reads the layers of the .cbddlp or .ctb copy (the relay keeps
  it), finds the outline of each layer at a step of some layers, and uploads the outlines as
  line loops in the same `path.bin` form. The page draws them in the color of the resin.
- Until a print has `path.bin`, the view shows the picture of the print (model.jpg) on a
  card over the bed.

## The API

`GET /api/v1/projects/<id>/printers/<printer>` returns:

- the printer (cloud.Printer: name, model, process, deck),
- the numbers of the project on this printer,
- the prints of the project on this printer, with the URLs of model.jpg and path.bin,
- the live tile of the printer, when it prints for the project.

The deck adds `path.bin` to the files of a print upload (the list of names in printDone).

## Build order

1. The cloud: the API route, the printer slugs, the page with the 3D view, the TAZ and Phenom
   GLB files, the "Printers" section and the links of the chips. No deck update.
2. The deck: `path.bin` for FDM prints, and the parts in the 3D view.
3. The deck: the outlines of resin prints.
