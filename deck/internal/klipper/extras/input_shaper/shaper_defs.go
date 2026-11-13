// Definitions of the supported input shapers.
//
// Ported to Go from klippy/extras/shaper_defs.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2020-2026  Dmitry Butyugin <dmbutyugin@google.com>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package input_shaper

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"printdeck/internal/klipper/mathutil"
)

const (
	ShaperVibrationReduction = 20.
	DefaultDampingRatio      = 0.1
)

// shaperArgs are the parameters in the name of a shaper, for example mzv(n=4, t=0.8).
type shaperArgs struct {
	list []float64
	kv   map[string]float64
}

// ShaperCfg is a shaper type (InputShaperCfg).
type ShaperCfg struct {
	Name            string
	initFunc        func(shaperFreq, dampingRatio float64, args shaperArgs) ([]float64, []float64, error)
	MinFreq         float64
	MaxDampingRatio float64
}

// ShaperError is an error of the parameters of a shaper.
type ShaperError struct{ Msg string }

func (e *ShaperError) Error() string { return e.Msg }

func shaperErrorf(format string, args ...any) error {
	return &ShaperError{fmt.Sprintf(format, args...)}
}

// GetNoneShaper is the shaper with no pulses.
func GetNoneShaper() ([]float64, []float64) { return []float64{}, []float64{} }

func getZVShaper(shaperFreq, dampingRatio float64, _ shaperArgs) ([]float64, []float64, error) {
	df := math.Sqrt(1. - dampingRatio*dampingRatio)
	K := math.Exp(-dampingRatio * math.Pi / df)
	tD := 1. / (shaperFreq * df)
	return []float64{1., K}, []float64{0., .5 * tD}, nil
}

func getZVDShaper(shaperFreq, dampingRatio float64, _ shaperArgs) ([]float64, []float64, error) {
	df := math.Sqrt(1. - dampingRatio*dampingRatio)
	K := math.Exp(-dampingRatio * math.Pi / df)
	tD := 1. / (shaperFreq * df)
	return []float64{1., 2. * K, K * K}, []float64{0., .5 * tD, tD}, nil
}

func getMZVCoeffs(n int, t float64) ([]float64, []float64, error) {
	if n < 3 {
		return nil, nil, shaperErrorf("Too small n=%d, must be at least 3", n)
	}
	fn := float64(n)
	if fn <= 2*t+1+1e-7 {
		return nil, nil, shaperErrorf("Too large t=%.6f for n=%d, must be less than %.6f", t, n, 0.5*(fn-1))
	}
	// Projected shaper duration with n -> \infinity for computing shaper zeros
	tau := t * (fn - 2.) / (fn - 2.*t - 1.)
	T := make([]float64, n)
	for i := range n {
		T[i] = float64(i) * t / (fn - 1.)
	}
	// Build a system of equations for A. The first equation is sum(A) = 1
	row := make([]float64, n)
	for i := range row {
		row[i] = 1.
	}
	M := [][]float64{row}
	F := []float64{1.}
	// Ensure correct placement of shaper zeros. Note that the system is not
	// over-contrained, as the extra equations are linearly-dependent.
	for i := 0; i < n-1; i++ {
		c, s := make([]float64, n), make([]float64, n)
		for j, tj := range T {
			w := 2. * math.Pi * (1. + float64(i)/tau) * tj
			c[j], s[j] = math.Cos(w), math.Sin(w)
		}
		M = append(M, c, s)
		F = append(F, 0., 0.)
	}
	MInv := mathutil.PseudoInverse(M)
	if MInv == nil {
		return nil, nil, shaperErrorf("Ill-formed shaper with n=%d, t=%.6f", n, t)
	}
	A := mathutil.MatMatMul([][]float64{F}, mathutil.MatTransp(MInv))[0]
	for _, a := range A {
		if a < -0.00001 {
			return nil, nil, shaperErrorf("Negative-valued shaper with n=%d, t=%.6f", n, t)
		}
	}
	return A, T, nil
}

func getMZVShaper(shaperFreq, dampingRatio float64, args shaperArgs) ([]float64, []float64, error) {
	n, t, tau := 3., 0.0, 0.0
	names := []string{"n", "t", "tau"}
	for i, v := range args.list {
		if i >= len(names) {
			return nil, nil, &ShaperError{fmt.Sprintf("get_mzv_shaper() takes from 2 to 5 positional arguments but %d were given", len(args.list)+2)}
		}
		args.kv[names[i]] = v
	}
	for k, v := range args.kv {
		switch k {
		case "n":
			n = v
		case "t":
			t = v
		case "tau":
			tau = v
		default:
			return nil, nil, &ShaperError{fmt.Sprintf("get_mzv_shaper() got an unexpected keyword argument '%s'", k)}
		}
	}
	if tau == 0 && t == 0 {
		t = 0.75
	} else if tau != 0 {
		// Infer total shaper duration from a projected shaper duration with
		// n -> \infinity
		t = tau * (n - 1.) / (n + 2.*tau - 2.)
	}
	A, T, err := getMZVCoeffs(int(n), t)
	if err != nil {
		return nil, nil, err
	}
	// Apply damping
	df := math.Sqrt(1. - dampingRatio*dampingRatio)
	K := math.Exp(-2. * t * dampingRatio * math.Pi / ((n - 1.) * df))
	tD := 1. / (shaperFreq * df)
	Kp := K
	for i := 1; i < int(n); i++ {
		T[i] *= tD
		A[i] *= Kp
		Kp *= K
	}
	return A, T, nil
}

func getEIShaper(shaperFreq, dampingRatio float64, _ shaperArgs) ([]float64, []float64, error) {
	vTol := 1. / ShaperVibrationReduction
	df := math.Sqrt(1. - dampingRatio*dampingRatio)
	tD := 1. / (shaperFreq * df)
	dr := dampingRatio

	a1 := (0.24968 + 0.24961*vTol) + ((0.80008+1.23328*vTol)+
		(0.49599+3.17316*vTol)*dr)*dr
	a3 := (0.25149 + 0.21474*vTol) + ((-0.83249+1.41498*vTol)+
		(0.85181-4.90094*vTol)*dr)*dr
	a2 := 1. - a1 - a3

	t2 := 0.4999 + (((0.46159+8.57843*vTol)*vTol)+
		(((4.26169-108.644*vTol)*vTol)+
			((1.75601+336.989*vTol)*vTol)*dr)*dr)*dr

	return []float64{a1, a2, a3}, []float64{0., t2 * tD, tD}, nil
}

func getShaperFromExpansionCoeffs(shaperFreq, dampingRatio float64, t, a [][]float64) ([]float64, []float64) {
	tau := 1. / shaperFreq
	var T, A []float64
	n := len(a)
	k := len(a[0])
	for i := range n {
		u := t[i][k-1]
		v := a[i][k-1]
		for j := 0; j < k-1; j++ {
			u = u*dampingRatio + t[i][k-j-2]
			v = v*dampingRatio + a[i][k-j-2]
		}
		T = append(T, u*tau)
		A = append(A, v)
	}
	return A, T
}

func get2HumpEIShaper(shaperFreq, dampingRatio float64, _ shaperArgs) ([]float64, []float64, error) {
	t := [][]float64{{0., 0., 0., 0.},
		{0.49890, 0.16270, -0.54262, 6.16180},
		{0.99748, 0.18382, -1.58270, 8.17120},
		{1.49920, -0.09297, -0.28338, 1.85710}}
	a := [][]float64{{0.16054, 0.76699, 2.26560, -1.22750},
		{0.33911, 0.45081, -2.58080, 1.73650},
		{0.34089, -0.61533, -0.68765, 0.42261},
		{0.15997, -0.60246, 1.00280, -0.93145}}
	A, T := getShaperFromExpansionCoeffs(shaperFreq, dampingRatio, t, a)
	return A, T, nil
}

func get3HumpEIShaper(shaperFreq, dampingRatio float64, _ shaperArgs) ([]float64, []float64, error) {
	t := [][]float64{{0., 0., 0., 0.},
		{0.49974, 0.23834, 0.44559, 12.4720},
		{0.99849, 0.29808, -2.36460, 23.3990},
		{1.49870, 0.10306, -2.01390, 17.0320},
		{1.99960, -0.28231, 0.61536, 5.40450}}
	a := [][]float64{{0.11275, 0.76632, 3.29160, -1.44380},
		{0.23698, 0.61164, -2.57850, 4.85220},
		{0.30008, -0.19062, -2.14560, 0.13744},
		{0.23775, -0.73297, 0.46885, -2.08650},
		{0.11244, -0.45439, 0.96382, -1.46000}}
	A, T := getShaperFromExpansionCoeffs(shaperFreq, dampingRatio, t, a)
	return A, T, nil
}

// InputShapers are the shapers. min_freq for each shaper is chosen to have projected max_accel
// ~= 1500.
var InputShapers = []ShaperCfg{
	{Name: "zv", initFunc: getZVShaper, MinFreq: 21., MaxDampingRatio: 0.99},
	{Name: "mzv", initFunc: getMZVShaper, MinFreq: 23., MaxDampingRatio: 0.99},
	{Name: "zvd", initFunc: getZVDShaper, MinFreq: 29., MaxDampingRatio: 0.99},
	{Name: "ei", initFunc: getEIShaper, MinFreq: 29., MaxDampingRatio: 0.4},
	{Name: "2hump_ei", initFunc: get2HumpEIShaper, MinFreq: 39., MaxDampingRatio: 0.3},
	{Name: "3hump_ei", initFunc: get3HumpEIShaper, MinFreq: 48., MaxDampingRatio: 0.2},
}

var (
	shaperNameRe = regexp.MustCompile(`^(\w+)\s*\((.*)\)$`)
	shaperArgRe  = regexp.MustCompile(`(?:(\w+)\s*=\s*)?\s*([\d.]+)`)
)

// GetShaperCfg returns the shaper type of a name, or nil.
func GetShaperCfg(shaperName string) *ShaperCfg {
	if m := shaperNameRe.FindStringSubmatch(shaperName); m != nil {
		shaperName = m[1]
	}
	for i := range InputShapers {
		if shaperName == InputShapers[i].Name {
			return &InputShapers[i]
		}
	}
	return nil
}

// InitShaper returns the pulses of a shaper (init_shaper). errorf makes the error of a bad
// shaper; with errorf nil, the error is a *ShaperError. A and T are nil for an unknown name.
func InitShaper(shaperName string, shaperFreq, dampingRatio float64, errorf func(string, ...any) error) (A, T []float64, err error) {
	wrap := func(e error) error {
		if errorf == nil {
			return e
		}
		return errorf("Failed to initialize shaper: %s", e.Error())
	}
	args := shaperArgs{kv: map[string]float64{}}
	if m := shaperNameRe.FindStringSubmatch(shaperName); m != nil {
		shaperName = m[1]
		if m[2] != "" {
			for _, a := range shaperArgRe.FindAllStringSubmatch(m[2], -1) {
				v, perr := parseVal(a[2])
				if perr != nil {
					return nil, nil, wrap(&ShaperError{perr.Error()})
				}
				if a[1] == "" {
					args.list = append(args.list, v)
				} else {
					args.kv[a[1]] = v
				}
			}
			if len(args.list) > 0 && len(args.kv) > 0 {
				return nil, nil, wrap(&ShaperError{"Mixing named and non-named shaper parameters is not supported"})
			}
		}
	}
	for _, s := range InputShapers {
		if shaperName == s.Name {
			A, T, err := s.initFunc(shaperFreq, dampingRatio, args)
			if err != nil {
				return nil, nil, wrap(err)
			}
			return A, T, nil
		}
	}
	return nil, nil, nil
}

// parseVal is parse_val of init_shaper: a number with a point is a float, else an int.
func parseVal(s string) (float64, error) {
	if strings.Contains(s, ".") {
		return strconv.ParseFloat(s, 64)
	}
	n, err := strconv.Atoi(s)
	return float64(n), err
}
