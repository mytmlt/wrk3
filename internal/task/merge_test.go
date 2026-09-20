package task

import (
	"reflect"
	"testing"
)

func TestMergeOverride(t *testing.T) {
	base, err := ParseCompose([]byte(`name: base
services:
  db:
    image: postgres:16
  app:
    image: app:1
    environment:
      LOG_LEVEL: info
    ports:
      - "8000:8080"
`))
	if err != nil {
		t.Fatal(err)
	}
	override, err := ParseCompose([]byte(`services:
  app:
    environment:
      LOG_LEVEL: debug
    ports:
      - "9000:8080"
  worker:
    image: worker:1
`))
	if err != nil {
		t.Fatal(err)
	}
	base.Merge(override)

	if base.Name != "base" {
		t.Errorf("Name = %q, want base", base.Name)
	}
	app := base.Services["app"]
	if app.Environment["LOG_LEVEL"] != "debug" {
		t.Errorf("override env not applied: %v", app.Environment)
	}
	if len(app.Ports) != 2 {
		t.Errorf("ports should append: %v", app.Ports)
	}
	if app.Image != "app:1" {
		t.Errorf("image should be preserved: %q", app.Image)
	}
	if _, ok := base.Services["worker"]; !ok {
		t.Error("worker service not merged in")
	}
	if _, ok := base.Services["db"]; !ok {
		t.Error("db service lost during merge")
	}
}

func TestUnionStrings(t *testing.T) {
	got := unionStrings([]string{"b", "a"}, []string{"b", "c", ""})
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unionStrings = %v, want %v", got, want)
	}
}
