// Ported to Go from klippy/extras/bed_mesh.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2018-2019 Eric Callahan <arksine.code@gmail.com>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package bed_mesh

import (
	"fmt"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/jinja"
)

type profile struct {
	points     [][]float64
	meshParams *MeshParams
}

// ProfileManager keeps the meshes of the [bed_mesh NAME] sections.
type ProfileManager struct {
	name                 string
	printer              *host.Printer
	gcode                *gcode.Dispatch
	bedmesh              *BedMesh
	profileNames         []string // in the order of a dict
	profiles             map[string]profile
	incompatibleProfiles []string
}

func newProfileManager(config *configfile.Wrapper, bedmesh *BedMesh) *ProfileManager {
	p := &ProfileManager{name: config.Name(), printer: host.Of(config), bedmesh: bedmesh,
		profiles: map[string]profile{}}
	p.gcode = gcode.Of(p.printer)
	// Fetch stored profiles from Config
	for _, prof := range config.PrefixSections(p.name) {
		if prof.Name() == p.name {
			continue
		}
		_, name, _ := strings.Cut(prof.Name(), " ")
		version := prof.GetInt("version", configfile.Default(0))
		if version != ProfileVersion {
			logf("bed_mesh: Profile [%s] not compatible with this version\n"+
				"of bed_mesh.  Profile Version: %d Current Version: %d ", name, version, ProfileVersion)
			p.incompatibleProfiles = append(p.incompatibleProfiles, name)
			continue
		}
		zvals := prof.GetFloatLists("points", []string{",", "\n"}, 0)
		params := jinja.NewDict()
		for _, opt := range ProfileOptions {
			switch opt.typ {
			case "int":
				params.Set(opt.name, prof.GetInt(opt.name))
			case "float":
				params.Set(opt.name, prof.GetFloat(opt.name))
			case "str":
				params.Set(opt.name, prof.Get(opt.name))
			}
		}
		p.setProfile(name, profile{zvals, params})
	}
	// Register GCode
	p.gcode.RegisterCommand("BED_MESH_PROFILE", p.cmdBedMeshProfile, false,
		"Bed Mesh Persistent Storage management")
	return p
}

func (p *ProfileManager) setProfile(name string, prof profile) {
	if _, ok := p.profiles[name]; !ok {
		p.profileNames = append(p.profileNames, name)
	}
	p.profiles[name] = prof
}

// profilesDict is the status value of the profiles.
func (p *ProfileManager) profilesDict() *jinja.Dict {
	d := jinja.NewDict()
	for _, name := range p.profileNames {
		prof := p.profiles[name]
		pd := jinja.NewDict()
		pd.Set("points", matrixList(prof.points))
		pd.Set("mesh_params", prof.meshParams)
		d.Set(name, pd)
	}
	return d
}

func (p *ProfileManager) pconfig() *configfile.PrinterConfig {
	return p.printer.LookupObject("configfile").(*configfile.PrinterConfig)
}

func (p *ProfileManager) checkIncompatibleProfiles() {
	if len(p.incompatibleProfiles) > 0 {
		for _, prof := range p.incompatibleProfiles {
			p.pconfig().RemoveSection("bed_mesh " + prof)
		}
		p.gcode.RespondInfo(fmt.Sprintf("The following incompatible profiles have been detected\n"+
			"and are scheduled for removal:\n%s\n"+
			"The SAVE_CONFIG command will update the printer config\n"+
			"file and restart the printer", strings.Join(p.incompatibleProfiles, "\n")), true)
	}
}

func (p *ProfileManager) saveProfile(profName string) {
	zMesh := p.bedmesh.Mesh()
	if zMesh == nil {
		p.gcode.RespondInfo(fmt.Sprintf("Unable to save to profile [%s], the bed has not been probed",
			profName), true)
		return
	}
	probedMatrix := zMesh.ProbedMatrix()
	meshParams := zMesh.MeshParams()
	cfgName := p.name + " " + profName
	// set params
	var zValues strings.Builder
	for _, line := range probedMatrix {
		zValues.WriteString("\n  ")
		for i, v := range line {
			if i > 0 {
				zValues.WriteString(", ")
			}
			fmt.Fprintf(&zValues, "%.6f", v)
		}
	}
	pc := p.pconfig()
	pc.Set(cfgName, "version", fmt.Sprint(ProfileVersion))
	pc.Set(cfgName, "points", zValues.String())
	for _, key := range meshParams.Keys() {
		v, _ := meshParams.Get(key)
		pc.Set(cfgName, key, pyStr(v))
	}
	// save copy in local storage
	p.setProfile(profName, profile{probedMatrix, copyParams(meshParams)})
	p.bedmesh.updateStatus()
	p.gcode.RespondInfo(fmt.Sprintf("Bed Mesh state has been saved to profile [%s]\n"+
		"for the current session.  The SAVE_CONFIG command will\n"+
		"update the printer config file and restart the printer.", profName), true)
}

func (p *ProfileManager) loadProfile(profName string) {
	prof, ok := p.profiles[profName]
	if !ok {
		panic(gcode.Errorf("bed_mesh: Unknown profile [%s]", profName))
	}
	zMesh := NewZMesh(prof.meshParams, profName)
	if err := zMesh.buildMesh(prof.points); err != nil {
		panic(gcode.Errorf("%s", err.Error()))
	}
	p.bedmesh.SetMesh(zMesh)
}

func (p *ProfileManager) removeProfile(profName string) {
	if _, ok := p.profiles[profName]; ok {
		p.pconfig().RemoveSection("bed_mesh " + profName)
		delete(p.profiles, profName)
		for i, n := range p.profileNames {
			if n == profName {
				p.profileNames = append(p.profileNames[:i:i], p.profileNames[i+1:]...)
				break
			}
		}
		p.bedmesh.updateStatus()
		p.gcode.RespondInfo(fmt.Sprintf("Profile [%s] removed from storage for this session.\n"+
			"The SAVE_CONFIG command will update the printer\n"+
			"configuration and restart the printer", profName), true)
	} else {
		p.gcode.RespondInfo(fmt.Sprintf("No profile named [%s] to remove", profName), true)
	}
}

func (p *ProfileManager) cmdBedMeshProfile(gcmd *gcode.Command) {
	options := []struct {
		key string
		fn  func(string)
	}{{"LOAD", p.loadProfile}, {"SAVE", p.saveProfile}, {"REMOVE", p.removeProfile}}
	for _, o := range options {
		name, ok := gcmd.Get(o.key, gcode.Default(nil))
		if ok {
			if strings.TrimSpace(name) == "" {
				panic(gcmd.Error("Value for parameter '%s' must be specified", o.key))
			}
			if name == "default" && o.key == "SAVE" {
				gcmd.RespondInfo("Profile 'default' is reserved, please choose"+
					" another profile name.", true)
			} else {
				o.fn(name)
			}
			return
		}
	}
	gcmd.RespondInfo(fmt.Sprintf("Invalid syntax '%s'", gcmd.Commandline()), true)
}
