package telemetry

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const envOverride = "WRK3_CONFIG_HOME"
const envConfigHome = "XDG_CONFIG_HOME"

type prefsData struct {
	Telemetry telemetryPrefs `yaml:"telemetry"`
}

type telemetryPrefs struct {
	Enabled  bool `yaml:"enabled"`
	Prompted bool `yaml:"prompted"`
}

func prefsPath() (string, error) {
	if v := os.Getenv(envOverride); v != "" {
		return filepath.Join(v, "wrk3", "preferences.yaml"), nil
	}
	if v := os.Getenv(envConfigHome); v != "" {
		return filepath.Join(v, "wrk3", "preferences.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve config home: %w", err)
	}
	return filepath.Join(home, ".config", "wrk3", "preferences.yaml"), nil
}

func LoadPrefs() (enabled bool, prompted bool) {
	path, err := prefsPath()
	if err != nil {
		return false, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, false
		}
		return false, false
	}
	if len(raw) == 0 {
		return false, false
	}
	var d prefsData
	if err := yaml.Unmarshal(raw, &d); err != nil {
		return false, false
	}
	return d.Telemetry.Enabled, d.Telemetry.Prompted
}

func SavePrefs(enabled, prompted bool) error {
	path, err := prefsPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create prefs dir %q: %w", dir, err)
	}
	d := prefsData{Telemetry: telemetryPrefs{Enabled: enabled, Prompted: prompted}}
	raw, err := yaml.Marshal(&d)
	if err != nil {
		return fmt.Errorf("encode prefs: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "preferences-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("create temp prefs file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp prefs file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp prefs file: %w", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("chmod temp prefs file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace prefs %q: %w", path, err)
	}
	return nil
}
