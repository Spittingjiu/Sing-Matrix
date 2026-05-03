package singbox

import (
	"encoding/json"
	"os"
	"testing"
)

func TestGenerateTestConfigFile(t *testing.T) {
	path := "config_test.json"
	defer os.Remove(path)
	if err := GenerateTestConfigFile(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Inbounds) != 2 || len(cfg.Outbounds) != 2 || cfg.Route.Final == "" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}
