package task

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseEnvironment(t *testing.T) {
	env, err := ParseEnvironment("Swarm")
	if err != nil {
		t.Fatal(err)
	}
	if env != EnvSwarm {
		t.Fatalf("got %q", env)
	}
	_, err = ParseEnvironment("k8s")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "compose") || !strings.Contains(err.Error(), "host") {
		t.Fatalf("error should list environments: %v", err)
	}
}

func TestParseShortPort(t *testing.T) {
	cases := []struct {
		in      string
		host    int
		cont    int
		hostVar string
		proto   string
	}{
		{"8000:80", 8000, 80, "", "tcp"},
		{"${APP_PORT:-8000}:8000", 8000, 8000, "APP_PORT", "tcp"},
		{"127.0.0.1:3000:3000", 3000, 3000, "", "tcp"},
		{"53:53/udp", 53, 53, "", "udp"},
		{"80", 80, 80, "", "tcp"},
	}
	for _, c := range cases {
		p, _, err := parseShortPort(c.in)
		if err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if p.Host != c.host || p.Container != c.cont || p.HostVar != c.hostVar || p.Protocol != c.proto {
			t.Fatalf("%s: %+v", c.in, p)
		}
	}
}

func TestAnalyzeComposeAndRender(t *testing.T) {
	dir := t.TempDir()
	compose := `name: demo
services:
  db:
    image: postgres:16-alpine
    container_name: demo-db
    environment:
      POSTGRES_PASSWORD: postgres
    ports:
      - "5432:5432"
    volumes:
      - pgdata:/var/lib/postgresql/data
  web:
    build: .
    ports:
      - "${APP_PORT:-8000}:8000"
    depends_on: [db]
    volumes:
      - .:/app
volumes:
  pgdata:
`
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}
	got, notes, err := Analyze(dir, AnalyzeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "demo" {
		t.Fatalf("name %q", got.Name)
	}
	if got.Source.Kind != SourceCompose {
		t.Fatalf("source %q", got.Source.Kind)
	}
	web, ok := got.LookupService("web")
	if !ok {
		t.Fatal("missing web")
	}
	if web.Build == nil || web.Build.Context != "." {
		t.Fatalf("web build %+v", web.Build)
	}
	if len(web.Ports) != 1 || web.Ports[0].HostVar != "APP_PORT" || web.Ports[0].Name != "app" {
		t.Fatalf("web ports %+v", web.Ports)
	}
	db, ok := got.LookupService("db")
	if !ok {
		t.Fatal("missing db")
	}
	if db.FixedContainerName != "demo-db" {
		t.Fatalf("container name %q", db.FixedContainerName)
	}
	if len(db.Ports) != 1 || db.Ports[0].Host != 5432 || db.Ports[0].HostVar == "" {
		t.Fatalf("db ports %+v", db.Ports)
	}
	foundCN := false
	for _, n := range notes {
		if n.Code == codeContainerName {
			foundCN = true
		}
	}
	if !foundCN {
		t.Fatalf("expected container_name note, got %v", notes)
	}

	internal, _, err := Render(*got, EnvInternal, notes)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(internal), "source files were not modified") {
		t.Fatalf("missing header: %s", internal)
	}
	parsed, err := Parse(internal)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Name != "demo" || len(parsed.Services) != 2 {
		t.Fatalf("parsed %+v", parsed)
	}

	comp, _, err := Render(*got, EnvCompose, notes)
	if err != nil {
		t.Fatal(err)
	}
	cs := string(comp)
	if strings.Contains(cs, "container_name") {
		t.Fatalf("compose projection must strip container_name: %s", cs)
	}
	if !strings.Contains(cs, "${APP_PORT:-8000}:8000") {
		t.Fatalf("compose should parameterize app port: %s", cs)
	}
	if !strings.Contains(cs, "${DB_PORT:-5432}:5432") && !strings.Contains(cs, "DB_PORT") {
		t.Fatalf("compose should parameterize db port: %s", cs)
	}

	host, _, err := Render(*got, EnvHost, notes)
	if err != nil {
		t.Fatal(err)
	}
	hs := string(host)
	if !strings.Contains(hs, "docker run") {
		t.Fatalf("host plan should docker run: %s", hs)
	}
	if !strings.Contains(hs, "postgres:16-alpine") {
		t.Fatalf("host plan missing image: %s", hs)
	}

	if err := os.WriteFile(filepath.Join(dir, "host.yaml"), host, 0o644); err != nil {
		t.Fatal(err)
	}
	fromHost, _, err := AnalyzeHost(dir, []string{"host.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	back, _, err := Render(*fromHost, EnvCompose, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(back), "postgres:16-alpine") {
		t.Fatalf("host→compose lost db image: %s", back)
	}

	_, swarmNotes, err := Render(*got, EnvSwarm, notes)
	if err == nil {
		t.Fatal("expected swarm to block on build-only web")
	}
	blocked := false
	for _, n := range swarmNotes {
		if n.Code == codeMissingImage && n.Level == levelBlock {
			blocked = true
		}
	}
	if !blocked {
		t.Fatalf("expected missing_image block, got %v", swarmNotes)
	}
}

func TestHostToCompose(t *testing.T) {
	dir := t.TempDir()
	plan := `name: local
services:
  app:
    command: python -m http.server 8000
    workdir: .
    port: 8000
  redis:
    image: redis:7
    port: 6379
`
	if err := os.WriteFile(filepath.Join(dir, "plan.yaml"), []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _, err := Analyze(dir, AnalyzeOptions{From: "host", Files: []string{"plan.yaml"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Source.Kind != SourceHost {
		t.Fatalf("kind %q", got.Source.Kind)
	}
	body, _, err := Render(*got, EnvCompose, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if !strings.Contains(s, "redis:7") {
		t.Fatalf("missing redis: %s", s)
	}
	if !strings.Contains(s, "python -m http.server 8000") {
		t.Fatalf("missing app command: %s", s)
	}
	if !strings.Contains(s, "build:") {
		t.Fatalf("app should get a build context: %s", s)
	}
}

func TestDiscoverComposeFiles(t *testing.T) {
	dir := t.TempDir()
	if files, err := DiscoverComposeFiles(dir); err != nil || files != nil {
		t.Fatalf("empty dir: %v %v", files, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := DiscoverComposeFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "docker-compose.yml" {
		t.Fatalf("got %v", files)
	}
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err = DiscoverComposeFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if files[0] != "compose.yaml" {
		t.Fatalf("prefer compose.yaml, got %v", files)
	}
}

func TestValidateRejectsEmpty(t *testing.T) {
	err := (Task{Name: "x"}).Validate()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSwarmRender(t *testing.T) {
	tk := Task{
		Name:   "demo",
		Source: Source{Kind: SourceCompose},
		Services: []Service{{
			Name:  "web",
			Image: "nginx:alpine",
			Ports: []Port{{Name: "app", Host: 8080, Container: 80, HostVar: "APP_PORT", Protocol: "tcp"}},
		}},
	}
	body, _, err := Render(tk, EnvSwarm, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, want := range []string{"overlay", "replicas: 1", "mode: ingress", "target: 80"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in %s", want, s)
		}
	}
}

func TestPortainerStripsContainerName(t *testing.T) {
	tk := Task{
		Name: "demo",
		Services: []Service{{
			Name:               "web",
			Image:              "nginx:alpine",
			FixedContainerName: "pinned",
			Ports:              []Port{{Name: "app", Host: 80, Container: 80, HostVar: "APP_PORT"}},
		}},
	}
	body, notes, err := Render(tk, EnvPortainer, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "container_name") || strings.Contains(string(body), "pinned") {
		t.Fatalf("portainer output kept container_name: %s", body)
	}
	found := false
	for _, n := range notes {
		if n.Code == codeContainerName {
			found = true
		}
	}
	if !found {
		t.Fatal("expected container_name note")
	}
}

func TestParseMounts(t *testing.T) {
	m, err := parseShortMount("./data:/data:ro")
	if err != nil {
		t.Fatal(err)
	}
	if m.Type != "bind" || m.Source != "./data" || m.Target != "/data" || !m.ReadOnly {
		t.Fatalf("%+v", m)
	}
	m, err = parseShortMount("pgdata:/var/lib/postgresql/data")
	if err != nil {
		t.Fatal(err)
	}
	if m.Type != "volume" || m.Source != "pgdata" {
		t.Fatalf("%+v", m)
	}
}

func TestMergeComposeOverride(t *testing.T) {
	dir := t.TempDir()
	base := `services:
  web:
    image: nginx:alpine
    ports: ["80:80"]
`
	over := `services:
  web:
    environment:
      FOO: bar
`
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "compose.override.yaml"), []byte(over), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _, err := Analyze(dir, AnalyzeOptions{From: "compose"})
	if err != nil {
		t.Fatal(err)
	}
	web, _ := got.LookupService("web")
	if web.Image != "nginx:alpine" || web.Env["FOO"] != "bar" {
		t.Fatalf("%+v", web)
	}
}
