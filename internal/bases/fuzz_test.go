package bases

import (
	"brain/internal/vault"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func FuzzEval(f *testing.F) {
	for _, s := range []string{`"a".repeat(2)`, `[1,2].slice(-1)`, `date("2024-01-01") + "1M"`, `price / 2`, `/a/g.matches("x")`, `note["x"].y(1)`, `file.links.map(value.asFile())`, `formula.a`, `this.file.name`, `{a: 1}.keys()`, `(1).toFixed(2)`, `"x".split("", 2)`, `list(1).reduce(acc+value, 0)`} {
		f.Add(s)
	}
	v := writeVaultF(f)
	f.Fuzz(func(t *testing.T, src string) {
		b := &Base{Formulas: map[string]string{"a": src, "b": "formula.a"}, Views: []View{{Type: "table", Name: "t", Order: []string{"formula.a", "formula.b"}, Summaries: map[string]string{"formula.a": "Sum"}}}}
		b.Filters = src
		b.Run(v, Options{This: "a.md", Now: time.Now()})
	})
}

func writeVaultF(f *testing.F) *vault.Vault {
	dir := f.TempDir()
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("---\nprice: 3\ntags: [x/y]\nup: \"[[b]]\"\n---\n[[b]] #t"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("---\nprice: abc\ndue: 2024-01-31\n---\n"), 0o644)
	v := vault.New(dir, "")
	v.Scan()
	return v
}
