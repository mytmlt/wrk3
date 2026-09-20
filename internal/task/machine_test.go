package task

import (
	"reflect"
	"strings"
	"testing"
)

func TestMachinePlanOrderAndNames(t *testing.T) {
	def := &Definition{
		Name: "demo",
		Services: map[string]*Service{
			"db": {
				Image:       "postgres:16-alpine",
				Environment: map[string]string{"POSTGRES_PASSWORD": "secret"},
				Volumes:     []Mount{{Type: MountVolume, Source: "db-data", Target: "/var/lib/postgresql/data"}},
				Ports:       []Port{{Published: 5432, Target: 5432, Protocol: ProtocolTCP}},
			},
			"app": {
				Image:     "myapp",
				DependsOn: []string{"db"},
				Networks:  []string{"backend"},
				Ports:     []Port{{Published: 8000, Target: 8080, Protocol: ProtocolTCP}},
			},
		},
		Volumes:  map[string]*Volume{"db-data": {}},
		Networks: map[string]*Network{"backend": {}},
	}
	plan, err := def.Machine(MachineOptions{Project: "demo"})
	if err != nil {
		t.Fatalf("Machine: %v", err)
	}
	if len(plan.Networks) != 1 || !reflect.DeepEqual(plan.Networks[0], []string{"docker", "network", "create", "demo_backend"}) {
		t.Errorf("Networks = %v", plan.Networks)
	}
	if len(plan.Up) != 2 {
		t.Fatalf("Up = %d, want 2", len(plan.Up))
	}
	if !strings.Contains(strings.Join(plan.Up[0], " "), "--name demo-db") {
		t.Errorf("first up should start db (dependency first): %v", plan.Up[0])
	}
	if !strings.Contains(strings.Join(plan.Up[1], " "), "--name demo-app") {
		t.Errorf("second up should start app: %v", plan.Up[1])
	}
	if !reflect.DeepEqual(plan.Down[0], []string{"docker", "rm", "-f", "demo-app"}) {
		t.Errorf("Down[0] = %v, want app first", plan.Down[0])
	}
	script := plan.Script()
	for _, want := range []string{"case \"$cmd\" in", "docker run -d --name demo-db", "docker rm -f demo-db", "docker network create demo_backend"} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q:\n%s", want, script)
		}
	}
}

func TestMachineRejectsBuildOnly(t *testing.T) {
	def, err := ParseCompose([]byte(sampleCompose))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := def.Machine(MachineOptions{}); err == nil || !strings.Contains(err.Error(), "no image") {
		t.Fatalf("Machine = %v, want no-image error", err)
	}
}

func TestMachineRoundTrip(t *testing.T) {
	def := &Definition{
		Services: map[string]*Service{
			"db": {
				Image:       "postgres:16-alpine",
				Restart:     "unless-stopped",
				Environment: map[string]string{"POSTGRES_PASSWORD": "secret"},
				Volumes:     []Mount{{Type: MountVolume, Source: "db-data", Target: "/var/lib/postgresql/data"}},
				Ports:       []Port{{Published: 5432, Target: 5432, Protocol: ProtocolTCP}},
			},
			"app": {
				Image:    "myapp:1.0",
				Networks: []string{"backend"},
				Ports:    []Port{{Published: 8000, Target: 8080, Protocol: ProtocolTCP}},
			},
		},
		Volumes:  map[string]*Volume{"db-data": {}},
		Networks: map[string]*Network{"backend": {}},
	}
	plan, err := def.Machine(MachineOptions{})
	if err != nil {
		t.Fatalf("Machine: %v", err)
	}
	got, err := ParseRunCommands([]byte(plan.Script()))
	if err != nil {
		t.Fatalf("ParseRunCommands: %v\n%s", err, plan.Script())
	}
	def.Normalize()
	if !reflect.DeepEqual(def, got) {
		t.Errorf("machine round trip mismatch:\nwant %+v\ngot  %+v", def, got)
	}
}

func TestParseRunCommands(t *testing.T) {
	script := `#!/bin/sh
docker network create backend
docker run -d --name db --restart unless-stopped -e POSTGRES_PASSWORD=secret -v db-data:/var/lib/postgresql/data -p 5432:5432 postgres:16-alpine
docker run -dit --name app --network backend -p 8000:8080 myapp:1.0 --flag "quoted arg"
`
	def, err := ParseRunCommands([]byte(script))
	if err != nil {
		t.Fatalf("ParseRunCommands: %v", err)
	}
	db := def.Services["db"]
	if db == nil {
		t.Fatalf("missing db service: %+v", def.Services)
	}
	if db.Image != "postgres:16-alpine" || db.Restart != "unless-stopped" {
		t.Errorf("db = %+v", db)
	}
	if db.Environment["POSTGRES_PASSWORD"] != "secret" {
		t.Errorf("db env = %+v", db.Environment)
	}
	if db.Volumes[0].Source != "db-data" || db.Volumes[0].Target != "/var/lib/postgresql/data" {
		t.Errorf("db volume = %+v", db.Volumes)
	}
	if db.Ports[0].Published != 5432 || db.Ports[0].Target != 5432 {
		t.Errorf("db port = %+v", db.Ports)
	}
	if _, ok := def.Volumes["db-data"]; !ok {
		t.Error("db-data volume not synthesized")
	}
	app := def.Services["app"]
	if app == nil {
		t.Fatalf("missing app service")
	}
	if !reflect.DeepEqual(app.Networks, []string{"backend"}) {
		t.Errorf("app networks = %v", app.Networks)
	}
	if def.Networks["backend"].External {
		t.Error("created network should be internal (External=false)")
	}
	if !reflect.DeepEqual(app.Command, []string{"--flag", "quoted arg"}) {
		t.Errorf("app command = %v", app.Command)
	}
}

func TestParseRunCommandsNoName(t *testing.T) {
	def, err := ParseRunCommands([]byte("docker run -d postgres:16-alpine\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := def.Services["postgres"]; !ok {
		t.Errorf("services = %v, want derived name postgres", def.Services)
	}
}

func TestParseRunCommandsErrors(t *testing.T) {
	if _, err := ParseRunCommands([]byte("# nothing here\n")); err == nil {
		t.Error("expected error for a script with no run commands")
	}
	if _, err := ParseRunCommands([]byte("docker run -d --name x\n")); err == nil {
		t.Error("expected error for a run command with no image")
	}
}

func TestNetworkCreateNameWithDriver(t *testing.T) {
	fields, err := shellFields("docker network create -d bridge mynet")
	if err != nil {
		t.Fatal(err)
	}
	name, ok := networkCreateName(fields)
	if !ok || name != "mynet" {
		t.Fatalf("networkCreateName = %q, %v; want mynet, true", name, ok)
	}
}

func TestShellFields(t *testing.T) {
	got, err := shellFields(`a "b c" 'd e' f\ g`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a", "b c", "d e", "f g"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("shellFields = %v, want %v", got, want)
	}
	if _, err := shellFields(`"unterminated`); err == nil {
		t.Error("expected unterminated quote error")
	}
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"simple":      "simple",
		"a b":         "'a b'",
		"":            "''",
		"it's":        `'it'\''s'`,
		"/path:/x:ro": "/path:/x:ro",
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}
