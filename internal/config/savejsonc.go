package config

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/tailscale/hujson"
)

const configFileMode = 0o600

func writeConfigFile(path string, data []byte) error {
	if err := os.WriteFile(path, data, configFileMode); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	if err := os.Chmod(path, configFileMode); err != nil {
		return fmt.Errorf("chmod config %s: %w", path, err)
	}
	return nil
}

func saveConfigCleanJSON(cfg *Config, path string) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return writeConfigFile(path, data)
}

func loadSaveBaseline(path string) (base []byte, oldCfg *Config, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultTemplate(), DefaultConfig(), nil
		}
		return nil, nil, fmt.Errorf("read config: %w", err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		return nil, nil, err
	}
	return data, cfg, nil
}

func configAsMap(cfg *Config) (map[string]any, error) {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("unmarshal config map: %w", err)
	}
	return m, nil
}

func buildConfigPatch(old, new *Config) ([]byte, error) {
	oldMap, err := configAsMap(old)
	if err != nil {
		return nil, err
	}
	newMap, err := configAsMap(new)
	if err != nil {
		return nil, err
	}
	var ops []map[string]any
	appendConfigDiff("", oldMap, newMap, &ops)
	if len(ops) == 0 {
		return nil, nil
	}
	return json.Marshal(ops)
}

func appendConfigDiff(path string, oldVal, newVal any, ops *[]map[string]any) {
	if reflect.DeepEqual(oldVal, newVal) {
		return
	}
	newMap, ok := newVal.(map[string]any)
	if ok {
		oldMap, _ := oldVal.(map[string]any)
		if oldMap == nil {
			oldMap = map[string]any{}
		}
		for k, nv := range newMap {
			p := joinJSONPointer(path, k)
			ov, exists := oldMap[k]
			if !exists {
				*ops = append(*ops, map[string]any{"op": "add", "path": p, "value": nv})
				continue
			}
			appendConfigDiff(p, ov, nv, ops)
		}
		return
	}
	op := "replace"
	if oldVal == nil {
		op = "add"
	}
	*ops = append(*ops, map[string]any{"op": op, "path": path, "value": newVal})
}

func joinJSONPointer(base, segment string) string {
	esc := strings.ReplaceAll(segment, "~", "~0")
	esc = strings.ReplaceAll(esc, "/", "~1")
	if base == "" {
		return "/" + esc
	}
	return base + "/" + esc
}

func saveConfigJSONC(cfg *Config, path string) error {
	base, oldCfg, err := loadSaveBaseline(path)
	if err != nil {
		return err
	}

	patch, err := buildConfigPatch(oldCfg, cfg)
	if err != nil {
		return err
	}
	if len(patch) == 0 {
		return nil
	}

	doc, err := hujson.Parse(base)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: parse %s for comment-preserving save: %v; writing clean JSON\n", path, err)
		return saveConfigCleanJSON(cfg, path)
	}

	if err := doc.Patch(patch); err != nil {
		fmt.Fprintf(os.Stderr, "warning: patch %s: %v; writing clean JSON\n", path, err)
		return saveConfigCleanJSON(cfg, path)
	}

	data := doc.Pack()
	return writeConfigFile(path, data)
}
