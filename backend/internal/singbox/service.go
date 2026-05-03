package singbox

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type Service struct {
	Bin             string
	RuntimeConfig   string
	GeneratedConfig string
}

func (s Service) ReadConfig() ([]byte, error) {
	return os.ReadFile(s.RuntimeConfig)
}

func (s Service) WriteGeneratedConfig(data []byte) error {
	if err := os.MkdirAll(filepath.Dir(s.GeneratedConfig), 0755); err != nil {
		return err
	}
	return os.WriteFile(s.GeneratedConfig, data, 0644)
}

func (s Service) Reload() error {
	if _, err := exec.LookPath(s.Bin); err != nil {
		return fmt.Errorf("sing-box binary not found: %w", err)
	}
	cmd := exec.Command(s.Bin, "check", "-c", s.GeneratedConfig)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("sing-box check failed: %s: %w", string(out), err)
	}
	return nil
}

type RealityKeys struct {
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
	ShortID    string `json:"short_id"`
	Note       string `json:"note"`
}

func GenerateRealityKeys(bin string) (RealityKeys, error) {
	short := make([]byte, 8)
	if _, err := rand.Read(short); err != nil {
		return RealityKeys{}, err
	}
	keys := RealityKeys{ShortID: hex.EncodeToString(short), Note: "Install sing-box to generate real x25519 keypair; short_id is cryptographically random."}
	if _, err := exec.LookPath(bin); err != nil {
		return keys, nil
	}
	out, err := exec.Command(bin, "generate", "reality-keypair").CombinedOutput()
	if err != nil {
		return keys, nil
	}
	lines := string(out)
	keys.Note = lines
	return keys, nil
}
