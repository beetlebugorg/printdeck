package mathutil

import (
	"math"
	"math/big"
)

// Hypot is Python's math.hypot of two values. Python 3.10 and later almost always round the
// result correctly, but Go's math.Hypot does not. This function rounds the exact result
// correctly.
func Hypot(x, y float64) float64 {
	switch {
	case math.IsInf(x, 0) || math.IsInf(y, 0):
		return math.Inf(1)
	case math.IsNaN(x) || math.IsNaN(y):
		return math.NaN()
	}
	const prec = 256
	bx := new(big.Float).SetPrec(prec).SetFloat64(x)
	by := new(big.Float).SetPrec(prec).SetFloat64(y)
	sum := new(big.Float).SetPrec(prec).Mul(bx, bx)
	sum.Add(sum, new(big.Float).SetPrec(prec).Mul(by, by))
	if sum.Sign() == 0 {
		return 0
	}
	f, _ := new(big.Float).SetPrec(prec).Sqrt(sum).Float64()
	return f
}
