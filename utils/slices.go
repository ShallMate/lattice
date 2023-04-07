package utils

import (
	"sort"

	"golang.org/x/exp/constraints"
)

// EqualSlice checks the equality between two slices of comparables.
func EqualSlice[V comparable](a, b []V) (v bool) {
	v = true
	for i := range a {
		v = v && (a[i] == b[i])
	}
	return
}

// MaxSlice returns the maximum value in the slice.
func MaxSlice[V constraints.Ordered](slice []V) (max V) {
	for _, c := range slice {
		max = Max(max, c)
	}
	return
}

// MinSlice returns the mininum value in the slice.
func MinSlice[V constraints.Ordered](slice []V) (min V) {
	for _, c := range slice {
		min = Min(min, c)
	}
	return
}

// IsInSlice checks if x is in slice.
func IsInSlice[V comparable](x V, slice []V) (v bool) {
	for i := range slice {
		v = v || (slice[i] == x)
	}
	return
}

// GetSortedKeys returns the sorted keys of a map.
func GetSortedKeys[K constraints.Ordered, V any](m map[K]V) (keys []K) {
	keys = make([]K, len(m))

	var i int
	for key := range m {
		keys[i] = key
		i++
	}

	SortSlice(keys)

	return
}

// GetDistincts returns the list distincts element in v.
func GetDistincts[V comparable](v []V) (vd []V) {
	m := map[V]bool{}
	for _, vi := range v {
		m[vi] = true
	}

	vd = make([]V, len(m))

	var i int
	for mi := range m {
		vd[i] = mi
		i++
	}

	return
}

// SortSlice sorts a slice in place.
func SortSlice[T constraints.Ordered](s []T) {
	sort.Slice(s, func(i, j int) bool {
		return s[i] < s[j]
	})
}

// RotateSlice returns a new slice corresponding to s rotated by k positions to the left.
func RotateSlice[V any](s []V, k int) []V {
	ret := make([]V, len(s))
	RotateSliceAllocFree(s, k, ret)
	return ret
}

// RotateSliceAllocFree rotates slice s by k positions to the left and writes the result in sout.
// without allocating new memory.
func RotateSliceAllocFree[V any](s []V, k int, sout []V) {

	if len(s) != len(sout) {
		panic("cannot RotateUint64SliceAllocFree: s and sout of different lengths")
	}

	if len(s) == 0 {
		return
	}

	k = k % len(s)
	if k < 0 {
		k = k + len(s)
	}

	if &s[0] == &sout[0] { // checks if the two slice share the same backing array
		RotateSliceInPlace(s, k)
		return
	}

	copy(sout[:len(s)-k], s[k:])
	copy(sout[len(s)-k:], s[:k])
}

// RotateSliceInPlace rotates slice s in place by k positions to the left.
func RotateSliceInPlace[V any](s []V, k int) {
	n := len(s)
	k = k % len(s)
	if k < 0 {
		k = k + len(s)
	}
	gcd := GCD(k, n)
	for i := 0; i < gcd; i++ {
		tmp := s[i]
		j := i
		for {
			x := j + k
			if x >= n {
				x = x - n
			}
			if x == i {
				break
			}
			s[j] = s[x]
			j = x
		}
		s[j] = tmp
	}
}

// RotateSlotsNew returns a new slice where the two half of the
// original slice are rotated each by k positions independently.
func RotateSlotsNew[V any](s []V, k int) (r []V) {
	r = make([]V, len(s))
	copy(r, s)
	slots := len(s) >> 1
	RotateSliceInPlace(r[:slots], k)
	RotateSliceInPlace(r[slots:], k)
	return
}
