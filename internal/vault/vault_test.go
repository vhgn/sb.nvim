package vault

import (
	"reflect"
	"testing"
)

func TestParseNoteEdgeCases(t *testing.T) {
	f := &File{Rel: "a.md"}
	ParseNote(f, "\ufeff---\nTags: [Proj/Sub]\ntag: one\n---\n#inline x#no #123 `#code`\n")
	if f.Props["Tags"] == nil {
		t.Fatalf("BOM hid the front matter: %#v", f.Props)
	}
	if want := []string{"Proj/Sub", "one", "inline"}; !reflect.DeepEqual(f.Tags, want) {
		t.Errorf("tags = %v, want %v", f.Tags, want)
	}

	g := &File{Rel: "b.md"}
	ParseNote(g, "---\nprice: 1\n#body\n")
	if g.Props != nil || !reflect.DeepEqual(g.Tags, []string{"body"}) {
		t.Errorf("unclosed front matter: props %v tags %v", g.Props, g.Tags)
	}
}
