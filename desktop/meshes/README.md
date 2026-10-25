# The meshes of the 3D window

The TAZ Workhorse meshes come from the CAD of the LulzBot TAZ Workhorse ("Redgum"):
https://gitlab.com/lulzbot3d/printers/redgum

The LulzBot Mini 2 meshes (mini2_*.bin) come from the CAD of the LulzBot Mini 2
(hibiscus_assembled.fcstd), with its stock LCD.

The CAD is by LulzBot. It has the licenses
"GPLv3 or later and/or CC BY-SA 4.0". These meshes have the same licenses.

cad/export.py makes the TAZ Workhorse meshes from the FreeCAD files of the CAD. The file has the steps.

The pod_*.bin meshes are the Printdeck pod of this repository (lulzbot/pod.py): the 5 inch Touch
Display 2 with the Pi 5 on its back, in its printed case. The taz_*.bin meshes are its bracket, the
rail clips and the cable clips on the TAZ Workhorse. The Mini 2 has its bracket and clips in
mini2_*.bin. cad/deck.py makes these meshes and the Mini 2 meshes, and deck.json lists them. The
file has the command. The pod meshes have the license of this repository.
