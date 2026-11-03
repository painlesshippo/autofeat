package templates

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPutGetAndNames(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	names, err := Names()
	if err != nil || len(names) != 0 {
		t.Fatalf("Names(empty) = (%v, %v), want empty slice", names, err)
	}

	want := Template{Repositories: []Repository{
		{Kind: LocalRepository, Source: "/sources/api"},
		{Kind: RemoteRepository, Source: "git@example.com:team/web.git"},
	}}
	if err := Put("full-stack", want); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if err := Put("backend", Template{Repositories: []Repository{{Kind: LocalRepository, Source: "/sources/api"}}}); err != nil {
		t.Fatalf("Put() second template error = %v", err)
	}
	got, err := Get("full-stack")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Get() = %+v, want %+v", got, want)
	}
	names, err = Names()
	if err != nil {
		t.Fatalf("Names() error = %v", err)
	}
	if !reflect.DeepEqual(names, []string{"backend", "full-stack"}) {
		t.Errorf("Names() = %v", names)
	}
	if _, err := Get("missing"); !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("Get(missing) error = %v, want ErrTemplateNotFound", err)
	}

	// Corrupt store file propagates errors through Put, Get, and Names.
	storePath, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storePath, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Put("broken", want); err == nil {
		t.Error("Put(corrupt) error = nil, want error")
	}
	if _, err := Get("full-stack"); err == nil {
		t.Error("Get(corrupt) error = nil, want error")
	}
	if _, err := Names(); err == nil {
		t.Error("Names(corrupt) error = nil, want error")
	}
}

func TestLoadFromPathAndSaveToPathValidation(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"corrupt json":            `{"templates":`,
		"trailing json":           `{"schema_version":1,"templates":{}} {}`,
		"unknown field":           `{"schema_version":1,"templates":{},"extra":true}`,
		"unsupported schema":      `{"schema_version":2,"templates":{}}`,
		"blank template name":     `{"schema_version":1,"templates":{" ":{"repositories":[{"kind":"local","source":"/repo"}]}}}`,
		"empty repositories":      `{"schema_version":1,"templates":{"empty":{"repositories":[]}}}`,
		"invalid repository kind": `{"schema_version":1,"templates":{"badkind":{"repositories":[{"kind":"svn","source":"/repo"}]}}}`,
		"blank repository source": `{"schema_version":1,"templates":{"blanksource":{"repositories":[{"kind":"local","source":" "}]}}}`,
		"relative local source":   `{"schema_version":1,"templates":{"relative":{"repositories":[{"kind":"local","source":"repo"}]}}}`,
		"duplicate local source":  `{"schema_version":1,"templates":{"dupe":{"repositories":[{"kind":"local","source":"/repo"},{"kind":"local","source":"/repo/."}]}}}`,
		"duplicate remote source": `{"schema_version":1,"templates":{"duperemote":{"repositories":[{"kind":"remote","source":"https://example.com/a.git"},{"kind":"remote","source":"https://example.com/a.git"}]}}}`,
	}
	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "templates.json")
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadFromPath(path); err == nil {
				t.Fatal("LoadFromPath() error = nil, want validation error")
			}
		})
	}

	if err := SaveToPath(filepath.Join(t.TempDir(), "templates.json"), Store{SchemaVersion: 99}); err == nil {
		t.Error("SaveToPath(unsupported version) error = nil, want error")
	}
	emptyFile := filepath.Join(t.TempDir(), "empty.json")
	if err := SaveToPath(emptyFile, Store{}); err != nil {
		t.Fatalf("SaveToPath(nil Templates map) error = %v", err)
	}
	loaded, err := LoadFromPath(emptyFile)
	if err != nil || loaded.Templates == nil {
		t.Errorf("LoadFromPath(emptyFile) = (%+v, %v), want non-nil Templates map", loaded, err)
	}
}
