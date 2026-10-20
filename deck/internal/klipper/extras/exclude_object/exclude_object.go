// Package exclude_object excludes the moves toward and inside the objects that the user cancels.
//
// Ported to Go from klippy/extras/exclude_object.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2019  Eric Callahan <arksine.code@gmail.com>
//	Copyright (C) 2021  Troy Jacobson <troy.d.jacobson@gmail.com>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package exclude_object

import (
	"fmt"
	"log"
	"sort"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/gcode_move"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/jinja"
	"printdeck/internal/klipper/toolhead"
)

type tuningTower interface{ IsActive() bool }

// ExcludeObject is the exclude_object object. It is a move transform while an object is
// excluded.
type ExcludeObject struct {
	printer               *host.Printer
	gcode                 *gcode.Dispatch
	gcodeMove             *gcode_move.GCodeMove
	toolhead              *toolhead.ToolHead
	nextTransform         gcode_move.MoveTransform
	lastPositionExtruded  []float64
	lastPositionExcluded  []float64
	extrusionOffsets      map[string][]float64
	maxPositionExtruded   float64
	maxPositionExcluded   float64
	extruderAdj           float64
	initialExtrusionMoves int
	lastPosition          []float64
	lastSpeed             float64
	objects               []*jinja.Dict
	excludedObjects       []string
	currentObject         *string
	inExcludedRegion      bool
	wasExcludedAtStart    bool
}

func load(config *configfile.Wrapper) any {
	printer := host.Of(config)
	e := &ExcludeObject{printer: printer, gcode: gcode.Of(printer)}
	printer.LoadObject(config, "gcode_move")
	e.gcodeMove = gcode_move.Of(printer)
	printer.RegisterEventHandler("klippy:connect", func(...any) { e.handleConnect() })
	printer.RegisterEventHandler("virtual_sdcard:reset_file", func(...any) { e.resetFile() })
	e.lastPositionExtruded = []float64{0., 0., 0., 0.}
	e.lastPositionExcluded = []float64{0., 0., 0., 0.}
	e.resetState()
	e.gcode.RegisterCommand("EXCLUDE_OBJECT_START", e.cmdExcludeObjectStart, false,
		"Marks the beginning the current object as labeled")
	e.gcode.RegisterCommand("EXCLUDE_OBJECT_END", e.cmdExcludeObjectEnd, false,
		"Marks the end the current object")
	e.gcode.RegisterCommand("EXCLUDE_OBJECT", e.cmdExcludeObject, false,
		"Cancel moves inside a specified objects")
	e.gcode.RegisterCommand("EXCLUDE_OBJECT_DEFINE", e.cmdExcludeObjectDefine, false,
		"Provides a summary of an object")
	return e
}

func init() { host.RegisterModule("exclude_object", load, nil) }

func (e *ExcludeObject) registerTransform() {
	if e.nextTransform == nil {
		tt := e.printer.LookupObject("tuning_tower").(tuningTower)
		if tt.IsActive() {
			log.Print("The ExcludeObject move transform is not being loaded due to Tuning tower being Active")
			return
		}
		e.nextTransform = e.gcodeMove.SetMoveTransform(e, true)
		e.extrusionOffsets = map[string][]float64{}
		e.maxPositionExtruded = 0
		e.maxPositionExcluded = 0
		e.extruderAdj = 0
		e.initialExtrusionMoves = 5
		e.lastPosition = []float64{0., 0., 0., 0.}
		e.Position()
		e.lastPositionExtruded = append([]float64(nil), e.lastPosition...)
		e.lastPositionExcluded = append([]float64(nil), e.lastPosition...)
	}
}

func (e *ExcludeObject) handleConnect() {
	e.toolhead = toolhead.Of(e.printer)
}

func (e *ExcludeObject) unregisterTransform() {
	if e.nextTransform != nil {
		tt := e.printer.LookupObject("tuning_tower").(tuningTower)
		if tt.IsActive() {
			log.Print("The Exclude Object move transform was not unregistered because it is not at the head " +
				"of the transform chain.")
			return
		}
		e.gcodeMove.SetMoveTransform(e.nextTransform, true)
		e.nextTransform = nil
		e.gcodeMove.ResetLastPosition()
	}
}

func (e *ExcludeObject) resetState() {
	e.objects = nil
	e.excludedObjects = nil
	e.currentObject = nil
	e.inExcludedRegion = false
}

func (e *ExcludeObject) resetFile() {
	e.resetState()
	e.unregisterTransform()
}

func (e *ExcludeObject) getExtrusionOffsets(numCoord int) []float64 {
	ename := e.toolhead.Extruder().Name()
	offset, ok := e.extrusionOffsets[ename]
	if !ok {
		offset = make([]float64, numCoord)
	}
	if len(offset) < numCoord {
		offset = append(offset, make([]float64, numCoord-len(offset))...)
	}
	e.extrusionOffsets[ename] = offset
	return offset
}

// Position is the position of the next transform, with the offsets of the excluded moves.
func (e *ExcludeObject) Position() []float64 {
	pos := e.nextTransform.Position()
	offset := e.getExtrusionOffsets(len(pos))
	for i := range pos {
		e.lastPosition[i] = pos[i] + offset[i]
	}
	return append([]float64(nil), e.lastPosition...)
}

// pyMax is Python's max() of two values.
func pyMax(a, b float64) float64 {
	if b > a {
		return b
	}
	return a
}

func (e *ExcludeObject) normalMove(newpos []float64, speed float64) {
	offset := e.getExtrusionOffsets(len(newpos))
	if e.initialExtrusionMoves > 0 && e.lastPosition[3] != newpos[3] {
		// Since the transform is not loaded until there is a request to
		// exclude an object, the transform needs to track a few extrusions
		// to get the state of the extruder
		e.initialExtrusionMoves--
	}
	e.lastPosition = append([]float64(nil), newpos...)
	e.lastPositionExtruded = append([]float64(nil), e.lastPosition...)
	e.maxPositionExtruded = pyMax(e.maxPositionExtruded, newpos[3])
	// These next few conditionals handle the moves immediately after leaving
	// and excluded object.  The toolhead is at the end of the last printed
	// object and the gcode is at the end of the last excluded object.
	//
	// Ideally, there will be Z and E moves right away to adjust any offsets
	// before moving away from the last position.  Any remaining corrections
	// will be made on the firs XY move.
	if (offset[0] != 0 || offset[1] != 0) &&
		(newpos[0] != e.lastPositionExcluded[0] || newpos[1] != e.lastPositionExcluded[1]) {
		for i := range newpos {
			if i != 3 {
				offset[i] = 0
			}
		}
		offset[3] += e.extruderAdj
		e.extruderAdj = 0
	}
	if offset[2] != 0 && newpos[2] != e.lastPositionExcluded[2] {
		offset[2] = 0
	}
	if e.extruderAdj != 0 && newpos[3] != e.lastPositionExcluded[3] {
		offset[3] += e.extruderAdj
		e.extruderAdj = 0
	}
	txPos := append([]float64(nil), newpos...)
	for i := range newpos {
		txPos[i] = newpos[i] - offset[i]
	}
	e.nextTransform.Move(txPos, speed)
}

func (e *ExcludeObject) ignoreMove(newpos []float64, speed float64) {
	offset := e.getExtrusionOffsets(len(newpos))
	for i := range newpos {
		if i != 3 {
			offset[i] = newpos[i] - e.lastPositionExtruded[i]
		}
	}
	offset[3] = offset[3] + newpos[3] - e.lastPosition[3]
	e.lastPosition = append([]float64(nil), newpos...)
	e.lastPositionExcluded = append([]float64(nil), e.lastPosition...)
	e.maxPositionExcluded = pyMax(e.maxPositionExcluded, newpos[3])
}

func (e *ExcludeObject) moveIntoExcludedRegion(newpos []float64, speed float64) {
	e.inExcludedRegion = true
	e.ignoreMove(newpos, speed)
}

func (e *ExcludeObject) moveFromExcludedRegion(newpos []float64, speed float64) {
	e.inExcludedRegion = false
	// This adjustment value is used to compensate for any retraction
	// differences between the last object printed and excluded one.
	e.extruderAdj = e.maxPositionExcluded - e.lastPositionExcluded[3] -
		(e.maxPositionExtruded - e.lastPositionExtruded[3])
	e.normalMove(newpos, speed)
}

func (e *ExcludeObject) testInExcludedRegion() bool {
	// Inside cancelled object
	if e.currentObject == nil {
		return false
	}
	for _, name := range e.excludedObjects {
		if name == *e.currentObject {
			return e.initialExtrusionMoves == 0
		}
	}
	return false
}

// Status is the objects, the excluded objects, and the current object.
func (e *ExcludeObject) Status(eventtime float64) map[string]any {
	objects := make([]any, len(e.objects))
	for i, o := range e.objects {
		objects[i] = o
	}
	excluded := make([]any, len(e.excludedObjects))
	for i, n := range e.excludedObjects {
		excluded[i] = n
	}
	var current any
	if e.currentObject != nil {
		current = *e.currentObject
	}
	return map[string]any{"objects": objects, "excluded_objects": excluded, "current_object": current}
}

// Move is a move of the transform chain.
func (e *ExcludeObject) Move(newpos []float64, speed float64) {
	moveInExcludedRegion := e.testInExcludedRegion()
	e.lastSpeed = speed
	if moveInExcludedRegion {
		if e.inExcludedRegion {
			e.ignoreMove(newpos, speed)
		} else {
			e.moveIntoExcludedRegion(newpos, speed)
		}
	} else {
		if e.inExcludedRegion {
			e.moveFromExcludedRegion(newpos, speed)
		} else {
			e.normalMove(newpos, speed)
		}
	}
}

func (e *ExcludeObject) cmdExcludeObjectStart(gcmd *gcode.Command) {
	name := strings.ToUpper(gcmd.GetString("NAME"))
	found := false
	for _, obj := range e.objects {
		if n, _ := obj.Get("name"); n == name {
			found = true
		}
	}
	if !found {
		d := jinja.NewDict()
		d.Set("name", name)
		e.addObjectDefinition(d)
	}
	e.currentObject = &name
	e.wasExcludedAtStart = e.testInExcludedRegion()
}

func (e *ExcludeObject) cmdExcludeObjectEnd(gcmd *gcode.Command) {
	if e.currentObject == nil && e.nextTransform != nil {
		gcmd.RespondInfo("EXCLUDE_OBJECT_END called, but no object is currently active", true)
		return
	}
	name, ok := gcmd.Get("NAME", configfile.Default(nil))
	if ok && (e.currentObject == nil || strings.ToUpper(name) != *e.currentObject) {
		current := "None"
		if e.currentObject != nil {
			current = *e.currentObject
		}
		gcmd.RespondInfo(fmt.Sprintf("EXCLUDE_OBJECT_END NAME=%s does not match the current object NAME=%s",
			strings.ToUpper(name), current), true)
	}
	e.currentObject = nil
}

func (e *ExcludeObject) cmdExcludeObject(gcmd *gcode.Command) {
	reset := gcmd.GetString("RESET", configfile.Default(""))
	current := gcmd.GetString("CURRENT", configfile.Default(""))
	name := strings.ToUpper(gcmd.GetString("NAME", configfile.Default("")))
	switch {
	case reset != "":
		if name != "" {
			e.unexcludeObject(name)
		} else {
			e.excludedObjects = nil
		}
	case name != "":
		if !contains(e.excludedObjects, strings.ToUpper(name)) {
			e.excludeObject(strings.ToUpper(name))
		}
	case current != "":
		if e.currentObject == nil || *e.currentObject == "" {
			panic(gcode.Errorf("There is no current object to cancel"))
		}
		e.excludeObject(*e.currentObject)
	default:
		e.listExcludedObjects(gcmd)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func jsonLoads(s string) any {
	v, err := jinja.JSONLoads(s)
	if err != nil {
		panic(err)
	}
	return v
}

func (e *ExcludeObject) cmdExcludeObjectDefine(gcmd *gcode.Command) {
	reset := gcmd.GetString("RESET", configfile.Default(""))
	name := strings.ToUpper(gcmd.GetString("NAME", configfile.Default("")))
	switch {
	case reset != "":
		e.resetFile()
	case name != "":
		params := gcmd.Params()
		obj := jinja.NewDict()
		obj.Set("name", strings.ToUpper(name))
		for _, k := range gcmd.ParamNames() {
			if k != "NAME" && k != "CENTER" && k != "POLYGON" {
				obj.Set(k, params[k])
			}
		}
		if center, ok := params["CENTER"]; ok {
			obj.Set("center", jsonLoads("["+center+"]"))
		}
		if polygon, ok := params["POLYGON"]; ok {
			obj.Set("polygon", jsonLoads(polygon))
		}
		e.addObjectDefinition(obj)
	default:
		e.listObjects(gcmd)
	}
}

func (e *ExcludeObject) addObjectDefinition(definition *jinja.Dict) {
	objects := append(append([]*jinja.Dict(nil), e.objects...), definition)
	sort.SliceStable(objects, func(i, j int) bool {
		a, _ := objects[i].Get("name")
		b, _ := objects[j].Get("name")
		return a.(string) < b.(string)
	})
	e.objects = objects
}

func (e *ExcludeObject) excludeObject(name string) {
	e.registerTransform()
	e.gcode.RespondInfo(fmt.Sprintf("Excluding object %s", strings.ToUpper(name)), true)
	if !contains(e.excludedObjects, name) {
		objs := append(append([]string(nil), e.excludedObjects...), name)
		sort.Strings(objs)
		e.excludedObjects = objs
	}
}

func (e *ExcludeObject) unexcludeObject(name string) {
	e.gcode.RespondInfo(fmt.Sprintf("Unexcluding object %s", strings.ToUpper(name)), true)
	for i, n := range e.excludedObjects {
		if n == name {
			objs := append(append([]string(nil), e.excludedObjects[:i]...), e.excludedObjects[i+1:]...)
			sort.Strings(objs)
			e.excludedObjects = objs
			break
		}
	}
}

func (e *ExcludeObject) listObjects(gcmd *gcode.Command) {
	var objectList string
	if _, ok := gcmd.Get("JSON", configfile.Default(nil)); ok {
		l := make([]any, len(e.objects))
		for i, o := range e.objects {
			l[i] = o
		}
		objectList = jinja.JSONDumps(l)
	} else {
		var names []string
		for _, o := range e.objects {
			n, _ := o.Get("name")
			names = append(names, n.(string))
		}
		objectList = strings.Join(names, " ")
	}
	gcmd.RespondInfo(fmt.Sprintf("Known objects: %s", objectList), true)
}

func (e *ExcludeObject) listExcludedObjects(gcmd *gcode.Command) {
	objectList := strings.Join(e.excludedObjects, " ")
	gcmd.RespondInfo(fmt.Sprintf("Excluded objects: %s", objectList), true)
}
