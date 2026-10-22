package printer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"
)

// Spool is a roll of filament. The filament on it is the weight on a scale less the weight of the
// empty reel. Each print subtracts the filament that it used.
type Spool struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"` // for example "Black eSun"
	Material string    `json:"material"`
	Net      float64   `json:"net"`   // grams of filament when the roll was last weighed
	Used     float64   `json:"used"`  // grams that prints used since then
	Full     float64   `json:"full"`  // grams of filament of the roll when it was added, for the price of a gram
	Reel     float64   `json:"reel"`  // grams of the empty reel
	Price    float64   `json:"price"` // the price of the roll, or 0
	Added    time.Time `json:"added"`
	// CuraMaterial is the ID of the material of Cura for the roll, or "": the base file of the
	// material, for example "generic_pla". Color is its color, for example "#ff7a1a", or "".
	CuraMaterial string `json:"cura_material,omitempty"`
	Color        string `json:"color,omitempty"`
}

// SlicerMaterial is a material of the slicer (a material of Cura, not a profile of print
// settings), for the rolls: the Cura plugin sends the materials that fit the printer. Material is
// its type, as Spool.Material.
type SlicerMaterial struct {
	ID       string `json:"id"`       // the base file of the material in Cura
	Name     string `json:"name"`     // for example "Microcenter White PLA"
	Brand    string `json:"brand"`    // for example "Microcenter"
	Material string `json:"material"` // the type, for example "PLA"
	Color    string `json:"color"`    // for example "#ffffff", or ""
}

// Left is the grams of filament that the roll has now.
func (s Spool) Left() float64 { return max(0, s.Net-s.Used) }

// Cost is the price of grams of filament of the roll, or 0 when the roll has no price.
func (s Spool) Cost(grams float64) float64 {
	if s.Price <= 0 || s.Full <= 0 {
		return 0
	}
	return grams * s.Price / s.Full
}

// Label is the name of the roll for the screen: its name, or its material.
func (s Spool) Label() string {
	if s.Name != "" {
		return s.Name
	}
	return s.Material
}

// Spools are the rolls of filament and the roll in the printer, in a JSON file.
type Spools struct {
	Path string // the file; "" keeps the rolls in memory only

	mu     sync.Mutex
	loaded bool
	data   struct {
		Spools []Spool `json:"spools"`
		In     string  `json:"in"`   // the ID of the roll in the printer, or ""
		Reel   float64 `json:"reel"` // the reel weight of the last new roll
		Next   int     `json:"next"` // the number of the next ID
		// The materials of the slicer, from the Cura plugin.
		SlicerMaterials []SlicerMaterial `json:"slicer_materials,omitempty"`
	}
}

func (s *Spools) load() {
	if s.loaded {
		return
	}
	s.loaded = true
	if s.Path == "" {
		return
	}
	if b, err := os.ReadFile(s.Path); err == nil {
		json.Unmarshal(b, &s.data)
	}
}

func (s *Spools) save() {
	if s.Path == "" {
		return
	}
	b, _ := json.Marshal(s.data)
	os.MkdirAll(filepath.Dir(s.Path), 0o700)
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err == nil {
		os.Rename(tmp, s.Path)
	}
}

// find returns the index of the roll with the ID, or -1. The caller holds s.mu.
func (s *Spools) find(id string) int {
	for i, sp := range s.data.Spools {
		if sp.ID == id {
			return i
		}
	}
	return -1
}

// List returns the rolls, the newest first.
func (s *Spools) List() []Spool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	out := make([]Spool, 0, len(s.data.Spools))
	for i := len(s.data.Spools) - 1; i >= 0; i-- {
		out = append(out, s.data.Spools[i])
	}
	return out
}

// Get returns the roll with the ID.
func (s *Spools) Get(id string) (Spool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	if i := s.find(id); i >= 0 {
		return s.data.Spools[i], true
	}
	return Spool{}, false
}

// In returns the roll in the printer.
func (s *Spools) In() (Spool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	if i := s.find(s.data.In); i >= 0 {
		return s.data.Spools[i], true
	}
	return Spool{}, false
}

// LastReel is the reel weight of the last new roll, or 0.
func (s *Spools) LastReel() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	return s.data.Reel
}

// Add adds a new roll with the weight on the scale, and puts it in the printer. It returns the roll.
func (s *Spools) Add(name, material string, scale, reel, price float64, now time.Time) Spool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	s.data.Next++
	net := max(0, scale-reel)
	sp := Spool{ID: strconv.Itoa(s.data.Next), Name: name, Material: material, Net: net, Full: net,
		Reel: reel, Price: price, Added: now}
	s.data.Spools = append(s.data.Spools, sp)
	s.data.In, s.data.Reel = sp.ID, reel
	s.save()
	return sp
}

// Put puts the roll with the ID in the printer. "" takes the roll out.
func (s *Spools) Put(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	s.data.In = id
	s.save()
}

// SetCuraMaterial sets the material of Cura of a roll, its color and its type. An empty material
// removes it.
func (s *Spools) SetCuraMaterial(id string, p SlicerMaterial) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	if i := s.find(id); i >= 0 {
		sp := &s.data.Spools[i]
		sp.CuraMaterial, sp.Color = p.ID, p.Color
		if p.Material != "" {
			sp.Material = p.Material
		}
		s.save()
	}
}

// SetSlicerMaterials keeps the materials of the slicer. It reports a change.
func (s *Spools) SetSlicerMaterials(list []SlicerMaterial) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	if slices.Equal(s.data.SlicerMaterials, list) {
		return false
	}
	s.data.SlicerMaterials = slices.Clone(list)
	s.save()
	return true
}

// SlicerMaterials are the materials of the slicer, as the Cura plugin sent them last.
func (s *Spools) SlicerMaterials() []SlicerMaterial {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	return slices.Clone(s.data.SlicerMaterials)
}

// SlicerMaterial is the material of the slicer with the ID.
func (s *Spools) SlicerMaterial(id string) (SlicerMaterial, bool) {
	for _, p := range s.SlicerMaterials() {
		if p.ID == id {
			return p, true
		}
	}
	return SlicerMaterial{}, false
}

// Weigh sets the filament of a roll from the weight on the scale.
func (s *Spools) Weigh(id string, scale float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	if i := s.find(id); i >= 0 {
		sp := &s.data.Spools[i]
		sp.Net, sp.Used = max(0, scale-sp.Reel), 0
		s.save()
	}
}

// Remove removes a roll, for example an empty one.
func (s *Spools) Remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	if i := s.find(id); i >= 0 {
		s.data.Spools = append(s.data.Spools[:i], s.data.Spools[i+1:]...)
		if s.data.In == id {
			s.data.In = ""
		}
		s.save()
	}
}

// Use subtracts the filament of a print from the roll in the printer. grams gives the weight of
// the filament for the material of the roll. It returns the roll and the cost of the filament,
// and false when no roll is in the printer.
func (s *Spools) Use(grams func(material string) float64) (Spool, float64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	i := s.find(s.data.In)
	if i < 0 {
		return Spool{}, 0, false
	}
	sp := &s.data.Spools[i]
	g := grams(sp.Material)
	sp.Used += g
	s.save()
	return *sp, sp.Cost(g), true
}
