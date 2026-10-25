// Package mathutil has the matrix helpers of Klipper: the least squares solution of a set of
// linear equations.
//
// Ported to Go from klippy/mathutil.py of Klipper (https://github.com/Klipper3d/klipper), commit
// 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f. The sums run in the order of the Python code.
//
//	Copyright (C) 2018-2019  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package mathutil

import (
	"math"
	"strconv"
)

// Matrix helper functions for NxM matrices

func dot(a, b []float64) float64 {
	s := 0.
	for i := range a {
		if i >= len(b) {
			break
		}
		s += a[i] * b[i]
	}
	return s
}

// MatTransp transposes a matrix.
func MatTransp(a [][]float64) [][]float64 {
	if len(a) == 0 {
		return nil
	}
	out := make([][]float64, len(a[0]))
	for i := range out {
		out[i] = make([]float64, len(a))
		for j := range a {
			out[i][j] = a[j][i]
		}
	}
	return out
}

// MatMatMul multiplies two matrices, or returns nil when their sizes do not fit.
func MatMatMul(a, b [][]float64) [][]float64 {
	if len(a[0]) != len(b) {
		return nil
	}
	bt := MatTransp(b)
	out := make([][]float64, len(a))
	for i, ai := range a {
		out[i] = make([]float64, len(bt))
		for j, btj := range bt {
			out[i][j] = dot(ai, btj)
		}
	}
	return out
}

// MatMulTransp is MatMatMul(a, MatTransp(a)).
func MatMulTransp(a [][]float64) [][]float64 {
	n := len(a)
	res := make([][]float64, n)
	// Resulting matrix is symmetric - compute lower-left
	for i := range a {
		res[i] = make([]float64, i+1, n)
		for j := 0; j <= i; j++ {
			res[i][j] = dot(a[i], a[j])
		}
	}
	// Fill in upper right of matrix
	for i := range res {
		for j := i + 1; j < n; j++ {
			res[i] = append(res[i], res[j][i])
		}
	}
	return res
}

// GaussianSolve solves m * x = rhs. It returns nil for a singular matrix, unless
// allowUnderdetermined is set.
func GaussianSolve(a, rhs [][]float64, allowUnderdetermined bool) [][]float64 {
	res := append([][]float64(nil), rhs...)
	m := append([][]float64(nil), a...)
	rows := len(m)
	// Perform the LU-decomposition through Gaussian elimination
	for i := rows - 1; i >= 0; i-- {
		// Find a pivot and swap the corresponding rows
		j, best := 0, math.Inf(-1)
		for k := 0; k <= i; k++ {
			if v := math.Abs(m[k][i]); v > best {
				j, best = k, v
			}
		}
		if i != j {
			m[i], m[j] = m[j], m[i]
			res[i], res[j] = res[j], res[i]
		}
		// Scale the i-th row (and drop last column)
		mi := m[i]
		var recipr float64
		if math.Abs(mi[i]) < 1e-10 {
			if !allowUnderdetermined {
				return nil
			}
			recipr = 0.
		} else {
			recipr = 1. / mi[i]
		}
		newMi := make([]float64, i)
		for k := 0; k < i; k++ {
			newMi[k] = mi[k] * recipr
		}
		m[i] = newMi
		newRes := make([]float64, len(res[i]))
		for k, v := range res[i] {
			newRes[k] = v * recipr
		}
		res[i] = newRes
		// Zero-out the last column in rows prior to i, and remove last column
		for j := 0; j < i; j++ {
			mj := m[j]
			c := mj[i]
			nm := make([]float64, len(newMi))
			for k := range newMi {
				nm[k] = mj[k] - c*newMi[k]
			}
			m[j] = nm
			nr := make([]float64, len(res[j]))
			for k := range res[j] {
				nr[k] = res[j][k] - c*newRes[k]
			}
			res[j] = nr
		}
	}
	// Solve the system with the lower-triangular matrix
	rest := MatTransp(res)
	if len(rest) == 0 {
		return res
	}
	for _, restK := range rest {
		for i := 1; i < rows; i++ {
			restK[i] -= dot(m[i], restK[:i])
		}
	}
	return MatTransp(rest)
}

// SolveLinearEquations returns the least squares solution of a set of linear equations, or nil.
func SolveLinearEquations(eqs, ans [][]float64, allowUnderdetermined bool) [][]float64 {
	eqst := MatTransp(eqs)
	return GaussianSolve(MatMulTransp(eqst), MatMatMul(eqst, ans), allowUnderdetermined)
}

// Round is Python's round(x, ndigits): the nearest value with ndigits decimals, from the exact
// value of x, with ties to even. strconv rounds the same way.
func Round(x float64, ndigits int) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return x
	}
	v, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'f', ndigits, 64), 64)
	return v
}
