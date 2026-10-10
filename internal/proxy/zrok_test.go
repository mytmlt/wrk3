package proxy

import (
	"testing"
)

func TestZrokName(t *testing.T) {
	if got := ZrokName("myt-home-", "Demo"); got != "myt-home-demo" {
		t.Errorf("ZrokName = %q", got)
	}
}

func TestZrokURL(t *testing.T) {
	if got := ZrokURL("myt-home-demo", "tunnel.mytmlt.site"); got != "https://myt-home-demo.tunnel.mytmlt.site" {
		t.Errorf("ZrokURL = %q", got)
	}
	if got := ZrokURL("myt-home-demo", "Tunnel.Mytmlt.Site."); got != "https://myt-home-demo.tunnel.mytmlt.site" {
		t.Errorf("ZrokURL normalize = %q", got)
	}
}

func TestValidateZrokName(t *testing.T) {
	for _, bad := range []string{"", "has space", "UPPER_ok?", "-lead", "trail-", "a/b"} {
		if err := ValidateZrokName(bad); err == nil {
			t.Errorf("ValidateZrokName(%q) = nil, want error", bad)
		}
	}
	if err := ValidateZrokName("myt-home-demo"); err != nil {
		t.Errorf("ValidateZrokName(valid) = %v", err)
	}
	long := ""
	for len(long) < 64 {
		long += "a"
	}
	if err := ValidateZrokName(long); err == nil {
		t.Error("overlong name should fail")
	}
}

func TestZrokShareArgs(t *testing.T) {
	args := ZrokShareArgs("http://127.0.0.1:6969", "public", "myt-home-demo", "")
	want := []string{"share", "public", "http://127.0.0.1:6969", "-n", "public:myt-home-demo", "--backend-mode", "proxy", "--headless", "--force-local"}
	if len(args) != len(want) {
		t.Fatalf("args = %v", args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args = %v, want %v", args, want)
		}
	}
}

func TestZrokBinOverride(t *testing.T) {
	t.Setenv("WRK3_ZROK_BIN", "stub-zrok2")
	if got := ZrokBin("zrok2"); got != "stub-zrok2" {
		t.Errorf("ZrokBin = %q", got)
	}
}

func TestZrokShareRunningMissing(t *testing.T) {
	if ZrokShareRunning(t.TempDir(), "demo") {
		t.Error("missing pid should not be running")
	}
}

func TestZrokShareIdentity(t *testing.T) {
	base := t.TempDir()
	if _, _, ok := ZrokShareIdentity(base, "demo"); ok {
		t.Error("missing pid should not be ok")
	}
	if err := ZrokWritePid(base, "demo", 12345, "myt-home-demo", "http://127.0.0.1:6969"); err != nil {
		t.Fatal(err)
	}
	name, target, ok := ZrokShareIdentity(base, "demo")
	if !ok || name != "myt-home-demo" || target != "http://127.0.0.1:6969" {
		t.Errorf("identity = (%q,%q,%v)", name, target, ok)
	}
}
