package onek

import (
	"sync/atomic"
	"testing"
)

func TestParallelForVisitsEveryIndexExactlyOnce(t *testing.T) {
	for _, n := range []int{0, 1, 2, 7, 1000} {
		counts := make([]atomic.Int32, n)
		parallelFor(n, func(i int) { counts[i].Add(1) })
		for i := range counts {
			if counts[i].Load() != 1 {
				t.Fatalf("n=%d: index %d visited %d times", n, i, counts[i].Load())
			}
		}
	}
}
