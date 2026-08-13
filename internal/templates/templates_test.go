package templates

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPutGetAndNames(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
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
	names, err := Names()
	if err != nil {
		t.Fatalf("Names() error = %v", err)
	}
	if !reflect.DeepEqual(names, []string{"backend", "full-stack"}) {
		t.Errorf("Names() = %v", names)
	}
	if _, err := Get("missing"); !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("Get(missing) error = %v, want ErrTemplateNotFound", err)
	}
}

func TestLoadFromPathRejectsInvalidTemplates(t *testing.T) {
	tests := map[string]string{
		"unknown field":         `{"schema_version":1,"templates":{},"extra":true}`,
		"unsupported schema":    `{"schema_version":2,"templates":{}}`,
		"empty repositories":    `{"schema_version":1,"templates":{"empty":{"repositories":[]}}}`,
		"relative local source": `{"schema_version":1,"templates":{"relative":{"repositories":[{"kind":"local","source":"repo"}]}}}`,
		"duplicate source":      `{"schema_version":1,"templates":{"dupe":{"repositories":[{"kind":"local","source":"/repo"},{"kind":"local","source":"/repo/."}]}}}`,
	}
	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "templates.json")
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadFromPath(path); err == nil {
				t.Fatal("LoadFromPath() error = nil, want validation error")
			}
		})
	}
}
