;FLAVOR:Marlin
;TIME:2041
;Filament used: 0.723506m
;Layer height: 0.25
;MINX:121.25
;MINY:132.25
;MINZ:0.25
;MAXX:158.75
;MAXY:147.75
;MAXZ:36
;TARGET_MACHINE.NAME:LulzBot TAZ Workhorse | SE | 0.5 mm

;Generated with Cura_SteamEngine 5.13.0
; Klipper start: wipe, washer probe and prime line run in the PRINT_START macro
PRINT_START BED=60 EXTRUDER=230 WIPE=160 LAYER=0.25 SPEED=18
M117 LTAZWSE05MM_tower-esun-pla-orange
M82 ;absolute extrusion mode
G92 E0
G92 E0
G1 F900 E-1.5
;LAYER_COUNT:144
;LAYER:0
M107
G0 F6000 X122.098 Y133.052 Z0.25
;TYPE:SKIRT
M104 S230
G1 X10 Y10 E1
G1 F900 E722.0059
M140 S0
M107
; Klipper end
PRINT_END
M82 ;absolute extrusion mode
M104 S0
;End of Gcode
