package proxy

import (
	"os"
	"runtime"
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

func TestZrokOwnsPID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ps-based check fail-opens on windows")
	}
	if ZrokOwnsPID(os.Getpid()) {
		t.Error("test binary is not a zrok share")
	}
	if ZrokOwnsPID(-1) {
		t.Error("invalid pid must not be owned")
	}
}

func TestZrokShareRunningStalePID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ps-based check fail-opens on windows")
	}
	base := t.TempDir()
	// Pid file pointing at this test process (not a zrok share) is stale:
	// Running reports false and drops the file so stop never kills it.
	if err := ZrokWritePid(base, "demo", os.Getpid(), "myt-home-demo", "http://127.0.0.1:6969", "public"); err != nil {
		t.Fatal(err)
	}
	if ZrokShareRunning(base, "demo") {
		t.Error("non-zrok pid must not count as running")
	}
	if _, _, _, ok := ZrokShareIdentity(base, "demo"); ok {
		t.Error("stale pid file should be removed")
	}
}

func TestZrokShareRunningMissing(t *testing.T) {
	if ZrokShareRunning(t.TempDir(), "demo") {
		t.Error("missing pid should not be running")
	}
}

func TestZrokShareIdentity(t *testing.T) {
	base := t.TempDir()
	if _, _, _, ok := ZrokShareIdentity(base, "demo"); ok {
		t.Error("missing pid should not be ok")
	}
	if err := ZrokWritePid(base, "demo", 12345, "myt-home-demo", "http://127.0.0.1:6969", "public"); err != nil {
		t.Fatal(err)
	}
	name, target, namespace, ok := ZrokShareIdentity(base, "demo")
	if !ok || name != "myt-home-demo" || target != "http://127.0.0.1:6969" || namespace != "public" {
		t.Errorf("identity = (%q,%q,%q,%v)", name, target, namespace, ok)
	}
}

func TestZrokShareIdentityLegacyNoNamespace(t *testing.T) {
	base := t.TempDir()
	// Pid files written before namespace tracking have no namespace field:
	// identity stays ok but namespace is empty so callers treat them as stale.
	if err := ZrokWritePid(base, "demo", 12345, "myt-home-demo", "http://127.0.0.1:6969", ""); err != nil {
		t.Fatal(err)
	}
	name, _, namespace, ok := ZrokShareIdentity(base, "demo")
	if !ok || name != "myt-home-demo" || namespace != "" {
		t.Errorf("legacy identity = (%q,%q,%v)", name, namespace, ok)
	}
}
