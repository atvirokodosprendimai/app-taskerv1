package view

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Datastar camel-cases the KEY of data-bind:x, data-indicator:x and their
// siblings, and HTML lower-cases it first — so a key-form signal name reaches
// the server spelled differently from the Go struct tag that reads it. Every
// server-side test still passes, because a test that posts JSON writes the key
// itself. This reads the templates, which is the only place the mistake exists.
func TestEveryDatastarSignalIsBoundByValueNotByKey(t *testing.T) {
	files, err := filepath.Glob("*.templ")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("found no .templ files; this gate would pass without looking at anything")
	}

	keyForm := regexp.MustCompile(`data-(bind|indicator|signals|computed|ref):[A-Za-z0-9_.-]+`)
	valueBinds := 0
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		valueBinds += strings.Count(string(src), "data-bind=")
		for _, m := range keyForm.FindAllString(string(src), -1) {
			t.Errorf("%s: %s uses the key form; bind by value, data-bind=\"name\"", f, m)
		}
	}
	if valueBinds == 0 {
		t.Fatal("found no data-bind at all; the gate is not reading the templates it guards")
	}
}
