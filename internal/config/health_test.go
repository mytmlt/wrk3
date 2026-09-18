package config

import (
	"strings"
	"testing"
	"time"
)

func TestHealthValidation(t *testing.T) {
	cases := []struct {
		name    string
		frag    string
		wantErr string
	}{
		{"absent ok", "", ""},
		{"happy", "health:\n  checks:\n    - name: api\n      run: \"curl -sf http://localhost:8000/healthz\"\n", ""},
		{"timeout ok", "health:\n  checks:\n    - name: api\n      run: \"true\"\n      timeout: 5s\n", ""},
		{"empty name", "health:\n  checks:\n    - name: \"\"\n      run: \"true\"\n", "health.checks[0].name"},
		{"empty run", "health:\n  checks:\n    - name: api\n      run: \"  \"\n", "health.checks[0].run"},
		{"dup name", "health:\n  checks:\n    - name: api\n      run: \"true\"\n    - name: api\n      run: \"true\"\n", "duplicated"},
		{"bad timeout", "health:\n  checks:\n    - name: api\n      run: \"true\"\n      timeout: soon\n", "health.checks[0].timeout"},
		{"timeout too small", "health:\n  checks:\n    - name: api\n      run: \"true\"\n      timeout: 100ms\n", "must be 1s-120s"},
		{"timeout too big", "health:\n  checks:\n    - name: api\n      run: \"true\"\n      timeout: 10m\n", "must be 1s-120s"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body := validBase + c.frag
			_, err := Load(writeConfig(t, body))
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("Load = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Load = nil, want error containing %q", c.wantErr)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("error %q should contain %q", err.Error(), c.wantErr)
			}
		})
	}
}

func TestHealthEffectiveTimeout(t *testing.T) {
	if got := (HealthCheck{Name: "a", Run: "true"}).EffectiveTimeout(); got != 10*time.Second {
		t.Errorf("default = %v, want 10s", got)
	}
	if got := (HealthCheck{Name: "a", Run: "true", Timeout: "5s"}).EffectiveTimeout(); got != 5*time.Second {
		t.Errorf("5s = %v, want 5s", got)
	}
	if got := (HealthCheck{Name: "a", Run: "true", Timeout: "bogus"}).EffectiveTimeout(); got != 10*time.Second {
		t.Errorf("bogus = %v, want 10s fallback", got)
	}
	if got := (HealthCheck{Name: "a", Run: "true", Timeout: "100ms"}).EffectiveTimeout(); got != time.Second {
		t.Errorf("100ms = %v, want clamped 1s", got)
	}
	if got := (HealthCheck{Name: "a", Run: "true", Timeout: "10m"}).EffectiveTimeout(); got != 120*time.Second {
		t.Errorf("10m = %v, want clamped 120s", got)
	}
}
