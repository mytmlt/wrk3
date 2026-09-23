package shared

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSlugVariants(t *testing.T) {
	cases := map[string][2]string{
		"feature-foo":     {"feature_foo", "feature-foo"},
		"feature/foo bar": {"feature_foo_bar", "feature-foo-bar"},
		"main":            {"main", "main"},
		"UPPER_Mixed-1":   {"upper_mixed_1", "upper-mixed-1"},
	}
	for in, want := range cases {
		if got := SlugUnderscore(in); got != want[0] {
			t.Errorf("SlugUnderscore(%q) = %q, want %q", in, got, want[0])
		}
		if got := SlugDash(in); got != want[1] {
			t.Errorf("SlugDash(%q) = %q, want %q", in, got, want[1])
		}
	}
}

func TestExpand(t *testing.T) {
	ctx := Context{Slug: "feature/foo", SharedProject: "demo-shared"}
	got := Expand("db_${slug_underscore}://${slug_dash}@${shared_project}/${slug}/${UNKNOWN_VAR}", ctx)
	want := "db_feature_foo://feature-foo@demo-shared/feature/foo/${UNKNOWN_VAR}"
	if got != want {
		t.Errorf("Expand = %q, want %q", got, want)
	}
	// Bare dollars pass through untouched (passwords, URLs).
	if got := Expand("amqp://guest:pa$$word@host:5672", ctx); got != "amqp://guest:pa$$word@host:5672" {
		t.Errorf("Expand bare dollars = %q", got)
	}
}

func TestExpandMapCopies(t *testing.T) {
	in := map[string]string{"A": "x_${slug}", "B": "plain"}
	got := ExpandMap(in, Context{Slug: "s"})
	if got["A"] != "x_s" || got["B"] != "plain" {
		t.Errorf("ExpandMap = %v", got)
	}
	if in["A"] != "x_${slug}" {
		t.Error("ExpandMap mutated input")
	}
	if ExpandMap(nil, Context{}) != nil {
		t.Error("ExpandMap(nil) should be nil")
	}
}

func TestSharedOverlay(t *testing.T) {
	raw, err := SharedOverlay(map[string]ServicePorts{
		"rabbitmq": {Ports: []string{"5672:5672", "15672:15672"}},
		"db":       {Ports: []string{"5432:5432"}},
		"vol":      {},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{"db:", "rabbitmq:", "5432:5432", "5672:5672", "15672:15672"} {
		if !strings.Contains(s, want) {
			t.Errorf("overlay missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "vol:") {
		t.Errorf("service without ports should be omitted:\n%s", s)
	}
	if raw2, err := SharedOverlay(map[string]ServicePorts{"db": {}}); err != nil || raw2 != nil {
		t.Errorf("no published ports should yield nil overlay: %v %v", raw2, err)
	}
}

func TestWorktreeOverlay(t *testing.T) {
	raw, err := WorktreeOverlay([]string{"app"}, map[string]string{"DB_HOST": "host.docker.internal"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{"app:", "environment:", "DB_HOST: host.docker.internal"} {
		if !strings.Contains(s, want) {
			t.Errorf("overlay missing %q:\n%s", want, s)
		}
	}
	if raw, err := WorktreeOverlay(nil, nil); err != nil || raw != nil {
		t.Errorf("empty services should yield nil overlay: %v %v", raw, err)
	}
}

func TestWriteFileIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".wrk3-overlays", "x.yml")
	if err := WriteFile(path, []byte("a: 1\n")); err != nil {
		t.Fatal(err)
	}
	info1, _ := os.Stat(path)
	if err := WriteFile(path, []byte("a: 1\n")); err != nil {
		t.Fatal(err)
	}
	info2, _ := os.Stat(path)
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Error("identical write should not churn mtime")
	}
	if err := WriteFile(path, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("nil raw should remove the file")
	}
}

func TestHostPorts(t *testing.T) {
	got := HostPorts([]string{
		"5432:5432",
		"127.0.0.1:5672:5672",
		"15672:15672/tcp",
		"8080",
		"notaport:5432",
		"",
		"1:2:3:4",
	})
	want := []int{5432, 5672, 15672}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("HostPorts = %v, want %v", got, want)
	}
}

func TestWaitReady(t *testing.T) {
	if err := WaitReady(nil); err != nil {
		t.Errorf("empty ports should return immediately: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	if err := WaitReadyWithTimeout([]int{port}, 5*time.Second); err != nil {
		t.Errorf("open port should pass: %v", err)
	}
	_ = ln.Close()
	if err := WaitReadyWithTimeout([]int{port}, 300*time.Millisecond); err == nil {
		t.Error("closed port should time out")
	} else if !strings.Contains(err.Error(), fmt.Sprint(port)) {
		t.Errorf("timeout error should name the port: %v", err)
	}
}
