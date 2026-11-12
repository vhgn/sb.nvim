package bases

import "sync"

// stableSort sorts s with exactly the algorithm of sort.SliceStable
// (insertion-sorted blocks of 20, then SymMerge passes), so that it yields
// the very same order even when less is not a strict weak ordering (as with
// mixed-type values). Operations on disjoint ranges run in parallel; less
// must be safe for concurrent use. less compares element values.
//
// The algorithm is adapted from Go's sort package (BSD license).
func stableSort(s []int, less func(x, y int) bool) {
	n := len(s)
	const blockSize = 20
	// Tasks of one pass work on disjoint ranges; run them in batches of
	// about batch elements.
	const batch = 1024
	type task struct{ a, m, b int }
	runPass := func(tasks []task, fn func(t task)) {
		par(n >= 2*batch, func(spawn func(func())) {
			for i := 0; i < len(tasks); {
				j, size := i, 0
				for j < len(tasks) && size < batch {
					size += tasks[j].b - tasks[j].a
					j++
				}
				ts := tasks[i:j]
				spawn(func() {
					for _, t := range ts {
						fn(t)
					}
				})
				i = j
			}
		})
	}
	var tasks []task
	a, b := 0, blockSize
	for b <= n {
		tasks = append(tasks, task{a, 0, b})
		a = b
		b += blockSize
	}
	tasks = append(tasks, task{a, 0, n})
	runPass(tasks, func(t task) { insertionSort(s, less, t.a, t.b) })
	for bs := blockSize; bs < n; bs *= 2 {
		tasks = tasks[:0]
		a, b = 0, 2*bs
		for b <= n {
			tasks = append(tasks, task{a, a + bs, b})
			a = b
			b += 2 * bs
		}
		if m := a + bs; m < n {
			tasks = append(tasks, task{a, m, n})
		}
		runPass(tasks, func(t task) { symMerge(s, less, t.a, t.m, t.b, 0) })
	}
}

// par runs the functions passed to spawn concurrently when parallel is
// true (and inline otherwise), returning once all of them finished.
func par(parallel bool, body func(spawn func(func()))) {
	if !parallel {
		body(func(f func()) { f() })
		return
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var panicked any
	body(func(f func()) {
		wg.Go(func() {
			defer func() {
				if x := recover(); x != nil {
					mu.Lock()
					if panicked == nil {
						panicked = x
					}
					mu.Unlock()
				}
			}()
			f()
		})
	})
	wg.Wait()
	if panicked != nil {
		panic(panicked)
	}
}

func insertionSort(s []int, less func(x, y int) bool, a, b int) {
	for i := a + 1; i < b; i++ {
		for j := i; j > a && less(s[j], s[j-1]); j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func symMerge(s []int, less func(x, y int) bool, a, m, b, depth int) {
	if m-a == 1 {
		i := m
		j := b
		for i < j {
			h := int(uint(i+j) >> 1)
			if less(s[h], s[a]) {
				i = h + 1
			} else {
				j = h
			}
		}
		for k := a; k < i-1; k++ {
			s[k], s[k+1] = s[k+1], s[k]
		}
		return
	}
	if b-m == 1 {
		i := a
		j := m
		for i < j {
			h := int(uint(i+j) >> 1)
			if !less(s[m], s[h]) {
				i = h + 1
			} else {
				j = h
			}
		}
		for k := m; k > i; k-- {
			s[k], s[k-1] = s[k-1], s[k]
		}
		return
	}
	mid := int(uint(a+b) >> 1)
	n := mid + m
	var start, r int
	if m > mid {
		start = n - b
		r = mid
	} else {
		start = a
		r = m
	}
	p := n - 1
	for start < r {
		c := int(uint(start+r) >> 1)
		if !less(s[p-c], s[c]) {
			start = c + 1
		} else {
			r = c
		}
	}
	end := n - start
	if start < m && m < end {
		rotate(s, start, m, end)
	}
	// The two halves are disjoint: merge them concurrently when large.
	par(b-a >= 4096 && depth < 6, func(spawn func(func())) {
		if a < start && start < mid {
			spawn(func() { symMerge(s, less, a, start, mid, depth+1) })
		}
		if mid < end && end < b {
			symMerge(s, less, mid, end, b, depth+1)
		}
	})
}

func rotate(s []int, a, m, b int) {
	i := m - a
	j := b - m
	for i != j {
		if i > j {
			swapRange(s, m-i, m, j)
			i -= j
		} else {
			swapRange(s, m-i, m+j-i, i)
			j -= i
		}
	}
	swapRange(s, m-i, m, i)
}

func swapRange(s []int, a, b, n int) {
	for i := 0; i < n; i++ {
		s[a+i], s[b+i] = s[b+i], s[a+i]
	}
}
