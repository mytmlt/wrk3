package task

import (
	"strings"
	"testing"
)

func TestToHost_NativeAndDockerRun(t *testing.T) {
	def := &Definition{
		Name: "demo",
		Services: []Service{
			{
				Name:       "app",
				Build:      &Build{Context: "."},
				Command:    Cmd{Argv: []string{"npm", "start"}},
				Env:        map[string]string{"NODE_ENV": "dev"},
				Ports:      []Port{{Published: "8000", Target: "8000"}},
				Mounts:     []Mount{{Type: "bind", Source: ".", Target: "/app"}},
				WorkingDir: "/app",
			},
			{
				Name:  "db",
				Image: "postgres:16-alpine",
				Env:   map[string]string{"POSTGRES_PASSWORD": "x"},
				Ports: []Port{{Published: "5432", Target: "5432"}},
			},
		},
	}
	plan, err := ToHost(def)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Processes) != 2 {
		t.Fatalf("processes = %d", len(plan.Processes))
	}
	app := plan.Processes[0]
	if app.Name != "app" || len(app.Command.Argv) != 2 || app.WorkingDir != "." {
		t.Errorf("app = %+v", app)
	}
	if app.Env["PORT"] != "8000" || app.Env["APP_PORT"] != "8000" {
		t.Errorf("app env = %v", app.Env)
	}
	if len(app.DockerArgs) != 0 {
		t.Errorf("app should be native, dockerArgs = %v", app.DockerArgs)
	}
	db := plan.Processes[1]
	if db.Image != "postgres:16-alpine" || len(db.DockerArgs) == 0 {
		t.Errorf("db = %+v", db)
	}
	joined := strings.Join(db.DockerArgs, " ")
	if !strings.Contains(joined, "postgres:16-alpine") || !strings.Contains(joined, "-p") {
		t.Errorf("docker args = %v", db.DockerArgs)
	}
}

func TestFromHost_ToCompose(t *testing.T) {
	plan := &HostPlan{
		Name: "local",
		Processes: []HostProcess{
			{
				Name:       "api",
				Command:    Cmd{Shell: "go run ."},
				Env:        map[string]string{"APP_ENV": "dev"},
				WorkingDir: ".",
				Ports:      []Port{{Published: "8000", Target: "8000"}},
			},
			{
				Name:  "redis",
				Image: "redis:7-alpine",
				Ports: []Port{{Published: "6379", Target: "6379"}},
			},
		},
	}
	def, err := FromHost(plan)
	if err != nil {
		t.Fatal(err)
	}
	if def.Source.Kind != SourceHost {
		t.Errorf("kind = %s", def.Source.Kind)
	}
	api := def.ServiceByName("api")
	if api == nil || api.Runtime != RuntimeHost || api.Build == nil {
		t.Fatalf("api = %+v", api)
	}
	if api.Command.Shell != "go run ." {
		t.Errorf("command = %+v", api.Command)
	}
	redis := def.ServiceByName("redis")
	if redis == nil || redis.Image != "redis:7-alpine" || redis.Runtime != RuntimeContainer {
		t.Errorf("redis = %+v", redis)
	}
	raw, err := ToCompose(def)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, "go run .") || !strings.Contains(s, "redis:7-alpine") {
		t.Errorf("compose =\n%s", s)
	}
}

func TestHostRoundTrip_ComposeToHostToCompose(t *testing.T) {
	dir := t.TempDir()
	writeCompose(t, dir, "docker-compose.yml", `
services:
  web:
    build: .
    command: python -m http.server 8000
    ports:
      - "8000:8000"
    volumes:
      - .:/app
    working_dir: /app
`)
	def, err := LoadCompose(dir, []string{"docker-compose.yml"})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := ToHost(def)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Processes) != 1 || plan.Processes[0].Command.Shell == "" {
		t.Fatalf("plan = %+v", plan)
	}
	back, err := FromHost(plan)
	if err != nil {
		t.Fatal(err)
	}
	web := back.ServiceByName("web")
	if web == nil || web.Command.Shell != "python -m http.server 8000" {
		t.Errorf("web = %+v", web)
	}
}

func TestFromHost_Errors(t *testing.T) {
	if _, err := FromHost(nil); err == nil {
		t.Fatal("nil plan")
	}
	if _, err := FromHost(&HostPlan{}); err == nil {
		t.Fatal("empty name")
	}
	if _, err := FromHost(&HostPlan{Name: "x"}); err == nil {
		t.Fatal("no processes")
	}
	if _, err := FromHost(&HostPlan{Name: "x", Processes: []HostProcess{{Name: ""}}}); err == nil {
		t.Fatal("empty process name")
	}
	if _, err := FromHost(&HostPlan{Name: "x", Processes: []HostProcess{{Name: "a"}, {Name: "a"}}}); err == nil {
		t.Fatal("duplicate")
	}
}
