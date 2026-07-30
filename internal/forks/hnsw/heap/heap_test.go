package heap

import (
	"math/rand"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

type Int int

func (i Int) Less(j Int) bool {
	return i < j
}

func TestHeap(t *testing.T) {
	h := Heap[Int]{}

	for i := 0; i < 20; i++ {
		h.Push(Int(rand.Int() % 100))
	}

	require.Equal(t, 20, h.Len())

	var inOrder []Int
	for h.Len() > 0 {
		inOrder = append(inOrder, h.Pop())
	}

	if !slices.IsSorted(inOrder) {
		t.Errorf("Heap did not return sorted elements: %+v", inOrder)
	}
}

// TestHeap_MaxAndPopLast is the IDX-1 regression: Max()/PopLast() must operate on
// the true maximum, not data[len-1] (an arbitrary leaf in a min-heap).
func TestHeap_MaxAndPopLast(t *testing.T) {
	for trial := 0; trial < 200; trial++ {
		h := Heap[Int]{}
		n := 1 + rand.Intn(30)
		vals := make([]Int, 0, n)
		for i := 0; i < n; i++ {
			v := Int(rand.Intn(1000))
			vals = append(vals, v)
			h.Push(v)
		}
		want := slices.Max(vals)
		require.Equal(t, want, h.Max(), "Max must be the true maximum")
		require.Equal(t, slices.Min(vals), h.Min(), "Min must be the true minimum")

		got := h.PopLast()
		require.Equal(t, want, got, "PopLast must remove and return the true maximum")
		require.Equal(t, n-1, h.Len())

		// Remaining elements: the multiset minus one instance of the max; heap
		// invariant intact (Pop still yields ascending order).
		var rest []Int
		for h.Len() > 0 {
			rest = append(rest, h.Pop())
		}
		require.True(t, slices.IsSorted(rest), "heap invariant broken after PopLast: %v", rest)
	}
}
