//go:build ignore

// Compare prints markdown tables comparing two `go test -bench` outputs,
// using the median of repeated runs.
//
//	go run bench/compare.go bench/results/before.txt bench/results/after.txt
package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type sample map[string][]float64 // unit → values

var lineRe = regexp.MustCompile(`^(Benchmark\S+?)(?:-\d+)?\s+\d+\s+(.*)$`)

func load(path string) (map[string]sample, []string) {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	out := map[string]sample{}
	var order []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		m := lineRe.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		name := strings.TrimPrefix(m[1], "Benchmark")
		if out[name] == nil {
			out[name] = sample{}
			order = append(order, name)
		}
		fs := strings.Fields(m[2])
		for i := 0; i+1 < len(fs); i += 2 {
			v, err := strconv.ParseFloat(fs[i], 64)
			if err == nil {
				out[name][fs[i+1]] = append(out[name][fs[i+1]], v)
			}
		}
	}
	return out, order
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return -1
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

func dur(ns float64) string {
	switch {
	case ns < 0:
		return "—"
	case ns >= 1e9:
		return fmt.Sprintf("%.2f s", ns/1e9)
	case ns >= 1e6:
		return fmt.Sprintf("%.2f ms", ns/1e6)
	case ns >= 1e3:
		return fmt.Sprintf("%.1f µs", ns/1e3)
	}
	return fmt.Sprintf("%.0f ns", ns)
}

func bytes(b float64) string {
	switch {
	case b < 0:
		return "—"
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MiB", b/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KiB", b/(1<<10))
	}
	return fmt.Sprintf("%.0f B", b)
}

func count(n float64) string {
	if n < 0 {
		return "—"
	}
	return strconv.FormatFloat(n, 'f', 0, 64)
}

func ratio(a, b float64) string {
	switch {
	case a < 0 || b < 0:
		return "new"
	case b == 0 && a == 0:
		return "="
	case b == 0:
		return "∞"
	}
	r := a / b
	if r >= 1.05 {
		if r >= 100 {
			return fmt.Sprintf("**%.0f×**", r)
		}
		return fmt.Sprintf("**%.1f×**", r)
	}
	if r <= 0.95 {
		return fmt.Sprintf("%.2f× (slower)", r)
	}
	return "≈"
}

func main() {
	before, _ := load(os.Args[1])
	after, order := load(os.Args[2])
	fmt.Println("| Benchmark | Before | After | Speedup | Mem before | Mem after | Allocs before | Allocs after |")
	fmt.Println("|---|--:|--:|--:|--:|--:|--:|--:|")
	for _, name := range order {
		b, a := before[name], after[name]
		get := func(s sample, unit string) float64 {
			if s == nil {
				return -1
			}
			return median(s[unit])
		}
		bt, at := get(b, "ns/op"), get(a, "ns/op")
		fmt.Printf("| %s | %s | %s | %s | %s | %s | %s | %s |\n", name,
			dur(bt), dur(at), ratio(bt, at),
			bytes(get(b, "B/op")), bytes(get(a, "B/op")),
			count(get(b, "allocs/op")), count(get(a, "allocs/op")))
	}
	var extra []string
	for _, name := range order {
		if b, a := before[name], after[name]; b != nil && len(b["resp-bytes"]) > 0 {
			extra = append(extra, fmt.Sprintf("| %s | %s | %s | %s |", name,
				bytes(median(b["resp-bytes"])), bytes(median(a["resp-bytes"])),
				ratio(median(b["resp-bytes"]), median(a["resp-bytes"]))))
		}
	}
	if len(extra) > 0 {
		fmt.Println("\n| Response size | Before | After | Smaller by |\n|---|--:|--:|--:|")
		fmt.Println(strings.Join(extra, "\n"))
	}
}
