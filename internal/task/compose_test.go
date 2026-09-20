package task

import (
	"reflect"
	"strings"
	"testing"
)

const sampleCompose = `name: demo
services:
  db:
    image: postgres:16-alpine
    environment:
      POSTGRES_PASSWORD: secret
    volumes:
      - db-data:/var/lib/postgresql/data
    ports:
      - "5432:5432"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres"]
      interval: 10s
      retries: 5
  app:
    build:
      context: ./app
      dockerfile: Dockerfile
    depends_on:
      - db
    environment:
      - DATABASE_URL=postgres://db:5432/app
      - SECRET_KEY
    ports:
      - target: 8080
        published: 8000
        protocol: tcp
        mode: host
    networks: [backend]
    deploy:
      replicas: 2
volumes:
  db-data:
networks:
  backend:
    driver: bridge
`

func TestParseComposeSyntaxes(t *testing.T) {
	def, err := ParseCompose([]byte(sampleCompose))
	if err != nil {
		t.Fatalf("ParseCompose: %v", err)
	}
	if def.Name != "demo" {
		t.Errorf("Name = %q, want demo", def.Name)
	}
	if len(def.Services) != 2 {
		t.Fatalf("services = %d, want 2", len(def.Services))
	}
	db := def.Services["db"]
	if db.Image != "postgres:16-alpine" {
		t.Errorf("db.Image = %q", db.Image)
	}
	if got := db.Volumes[0]; got.Type != MountVolume || got.Source != "db-data" || got.Target != "/var/lib/postgresql/data" {
		t.Errorf("db volume = %+v", got)
	}
	if got := db.Ports[0]; got.Published != 5432 || got.Target != 5432 || got.Protocol != ProtocolTCP {
		t.Errorf("db port = %+v", got)
	}
	if db.Healthcheck == nil || !reflect.DeepEqual(db.Healthcheck.Test, []string{"CMD-SHELL", "pg_isready -U postgres"}) {
		t.Errorf("db healthcheck = %+v", db.Healthcheck)
	}
	if db.Healthcheck.Retries != 5 || db.Healthcheck.Interval != "10s" {
		t.Errorf("db healthcheck timing = %+v", db.Healthcheck)
	}

	app := def.Services["app"]
	if app.Build == nil || app.Build.Context != "./app" || app.Build.Dockerfile != "Dockerfile" {
		t.Errorf("app.Build = %+v", app.Build)
	}
	if app.Environment["DATABASE_URL"] != "postgres://db:5432/app" {
		t.Errorf("app env DATABASE_URL = %q", app.Environment["DATABASE_URL"])
	}
	if app.Environment["SECRET_KEY"] != "${SECRET_KEY}" {
		t.Errorf("app bare env = %q, want ${SECRET_KEY}", app.Environment["SECRET_KEY"])
	}
	if !reflect.DeepEqual(app.DependsOn, []string{"db"}) {
		t.Errorf("app.DependsOn = %v", app.DependsOn)
	}
	if !reflect.DeepEqual(app.Networks, []string{"backend"}) {
		t.Errorf("app.Networks = %v", app.Networks)
	}
	if got := app.Ports[0]; got.Target != 8080 || got.Published != 8000 || got.Mode != ModeHost {
		t.Errorf("app port = %+v", got)
	}
	if app.Deploy == nil || app.Deploy.Replicas != 2 {
		t.Errorf("app.Deploy = %+v", app.Deploy)
	}
	if def.Networks["backend"].Driver != "bridge" {
		t.Errorf("backend driver = %q", def.Networks["backend"].Driver)
	}
}

func TestComposeRoundTrip(t *testing.T) {
	def, err := ParseCompose([]byte(sampleCompose))
	if err != nil {
		t.Fatalf("ParseCompose: %v", err)
	}
	raw, err := def.Compose()
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	got, err := ParseCompose(raw)
	if err != nil {
		t.Fatalf("ParseCompose(round trip): %v\n%s", err, raw)
	}
	if !reflect.DeepEqual(def, got) {
		t.Errorf("round trip mismatch:\nwant %+v\ngot  %+v\nrendered:\n%s", def, got, raw)
	}
}

func TestComposeRenderDeterministic(t *testing.T) {
	def, err := ParseCompose([]byte(sampleCompose))
	if err != nil {
		t.Fatal(err)
	}
	a, err := def.Compose()
	if err != nil {
		t.Fatal(err)
	}
	b, err := def.Compose()
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Errorf("Compose output is not deterministic:\n%s\n---\n%s", a, b)
	}
}

func TestParsePortString(t *testing.T) {
	cases := []struct {
		in   string
		want Port
	}{
		{"80", Port{Published: 80, Target: 80, Protocol: ProtocolTCP}},
		{"8080:80", Port{Published: 8080, Target: 80, Protocol: ProtocolTCP}},
		{"8080:80/udp", Port{Published: 8080, Target: 80, Protocol: ProtocolUDP}},
		{"127.0.0.1:8080:80", Port{HostIP: "127.0.0.1", Published: 8080, Target: 80, Protocol: ProtocolTCP}},
		{"[::1]:8080:80", Port{HostIP: "::1", Published: 8080, Target: 80, Protocol: ProtocolTCP}},
	}
	for _, c := range cases {
		got, err := parsePortString(c.in)
		if err != nil {
			t.Errorf("parsePortString(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parsePortString(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
	if _, err := parsePortString("nope"); err == nil {
		t.Error("parsePortString(nope) = nil error, want error")
	}
}

func TestParseMountString(t *testing.T) {
	cases := []struct {
		in   string
		want Mount
	}{
		{"/data", Mount{Type: MountVolume, Target: "/data"}},
		{"named:/data", Mount{Type: MountVolume, Source: "named", Target: "/data"}},
		{"./host:/data:ro", Mount{Type: MountBind, Source: "./host", Target: "/data", ReadOnly: true}},
		{"/abs/host:/data", Mount{Type: MountBind, Source: "/abs/host", Target: "/data"}},
	}
	for _, c := range cases {
		got, err := parseMountString(c.in)
		if err != nil {
			t.Errorf("parseMountString(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseMountString(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestValidateErrors(t *testing.T) {
	cases := []struct {
		name string
		def  *Definition
		want string
	}{
		{
			name: "no image or build",
			def: &Definition{Services: map[string]*Service{
				"app": {},
			}},
			want: "image or build",
		},
		{
			name: "bad target port",
			def: &Definition{Services: map[string]*Service{
				"app": {Image: "x", Ports: []Port{{Target: 99999}}},
			}},
			want: "out of range",
		},
		{
			name: "unknown dependency",
			def: &Definition{Services: map[string]*Service{
				"app": {Image: "x", DependsOn: []string{"db"}},
			}},
			want: "unknown service",
		},
		{
			name: "unknown network",
			def: &Definition{Services: map[string]*Service{
				"app": {Image: "x", Networks: []string{"backend"}},
			}},
			want: "unknown network",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.def.Validate()
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("Validate = %v, want containing %q", err, c.want)
			}
		})
	}
}

func TestOrderDependenciesFirst(t *testing.T) {
	def := &Definition{Services: map[string]*Service{
		"web": {Image: "web", DependsOn: []string{"api"}},
		"api": {Image: "api", DependsOn: []string{"db"}},
		"db":  {Image: "db"},
	}}
	want := []string{"db", "api", "web"}
	if got := def.Order(); !reflect.DeepEqual(got, want) {
		t.Errorf("Order = %v, want %v", got, want)
	}
}
