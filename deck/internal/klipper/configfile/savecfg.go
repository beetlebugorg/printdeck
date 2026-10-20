package configfile

// Config auto save: the changes of the SAVE_CONFIG command (the ConfigAutoSave class of
// configfile.py).

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

// Set sets an option of the SAVE_CONFIG block.
func (pc *PrinterConfig) Set(section, option, value string) {
	if pc.autosave == nil {
		pc.autosave = NewFileConfig()
	}
	if !pc.autosave.HasSection(section) {
		pc.autosave.AddSection(section)
	}
	pc.autosave.Set(section, option, value)
	pending := map[string]map[string]string{}
	for k, v := range pc.statusSavePending {
		pending[k] = v
	}
	opts := map[string]string{}
	for k, v := range pending[section] {
		opts[k] = v
	}
	opts[option] = value
	pending[section] = opts
	pc.statusSavePending = pending
	pc.saveConfigPending = true
	log.Printf("save_config: set [%s] %s = %s", section, option, value)
}

// RemoveSection removes a section of the SAVE_CONFIG block.
func (pc *PrinterConfig) RemoveSection(section string) {
	if pc.autosave == nil {
		return
	}
	pending := map[string]map[string]string{}
	for k, v := range pc.statusSavePending {
		pending[k] = v
	}
	if pc.autosave.HasSection(section) {
		pc.autosave.RemoveSection(section)
		pending[section] = nil
	} else if v, ok := pending[section]; ok && v != nil {
		delete(pending, section)
	} else {
		return
	}
	pc.statusSavePending = pending
	pc.saveConfigPending = true
}

// SaveConfig writes the SAVE_CONFIG block into the config file cfgname, after a backup of the
// file. It returns true when the printer must restart. The errors are the messages of the
// SAVE_CONFIG command.
func (pc *PrinterConfig) SaveConfig(cfgname string) (bool, error) {
	if pc.autosave == nil || len(pc.autosave.Sections()) == 0 {
		return false, nil
	}
	// Create string containing autosave data
	var lines []string
	for _, l := range strings.Split(pc.autosave.String(), "\n") {
		lines = append(lines, strings.TrimSpace("#*# "+l))
	}
	lines = append([]string{"\n" + strings.TrimRight(autosaveHeader, " \t\n")}, lines...)
	lines = append(lines, "")
	autosaveData := strings.Join(lines, "\n")
	// Read in and validate current config file
	data, err := ReadConfigFile(cfgname)
	if err != nil {
		log.Printf("Unable to read existing config on SAVE_CONFIG: %v", err)
		return false, fmt.Errorf("Unable to read existing config on SAVE_CONFIG")
	}
	regularData, _ := findAutosaveData(data)
	regularData = stripDuplicates(regularData, pc.autosave)
	data = strings.TrimRight(regularData, " \t\n\r") + autosaveData
	newRegularData, newAutosaveData := findAutosaveData(data)
	if newAutosaveData == "" {
		return false, fmt.Errorf("Existing config autosave is corrupted. Can't complete SAVE_CONFIG")
	}
	regular, err := BuildFileconfigWithIncludes(newRegularData, cfgname)
	if err != nil {
		log.Printf("Unable to parse existing config on SAVE_CONFIG: %v", err)
		return false, fmt.Errorf("Unable to parse existing config on SAVE_CONFIG")
	}
	for _, section := range pc.autosave.Sections() {
		for _, option := range pc.autosave.Options(section) {
			if regular.HasOption(section, option) {
				return false, fmt.Errorf("SAVE_CONFIG section '%s' option '%s' conflicts with included value",
					section, option)
			}
		}
	}
	// Determine filenames
	datestr := time.Now().Format("-20060102_150405")
	backupName := cfgname + datestr
	tempName := cfgname + "_autosave"
	if strings.HasSuffix(cfgname, ".cfg") {
		backupName = cfgname[:len(cfgname)-4] + datestr + ".cfg"
		tempName = cfgname[:len(cfgname)-4] + "_autosave.cfg"
	}
	// Create new config file with temporary name and swap with main config
	log.Printf("SAVE_CONFIG to '%s' (backup in '%s')", cfgname, backupName)
	if err := os.WriteFile(tempName, []byte(data), 0o644); err == nil {
		if err = os.Rename(cfgname, backupName); err == nil {
			err = os.Rename(tempName, cfgname)
		}
		if err == nil {
			return true, nil
		}
		log.Printf("Unable to write config file during SAVE_CONFIG: %v", err)
	}
	return false, fmt.Errorf("Unable to write config file during SAVE_CONFIG")
}
