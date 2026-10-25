package mathutil

import "math"

// Sum is Python's sum() of floats. Python 3.12 and later add a compensation term (Neumaier
// summation), so the result can differ from a plain loop in the last digits.
func Sum(values ...float64) float64 {
	result, c := 0., 0.
	for _, x := range values {
		t := result + x
		if math.Abs(result) >= math.Abs(x) {
			c += (result - t) + x
		} else {
			c += (x - t) + result
		}
		result = t
	}
	// Keep the sign of a negative zero, and keep an infinite sum
	if c != 0 && !math.IsInf(c, 0) && !math.IsNaN(c) {
		result += c
	}
	return result
}
