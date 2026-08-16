package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFeishuConfigBackwardCompatibleAndNoSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), configName)
	if err := os.WriteFile(path, []byte(`{"feishu_app_id":"app","feishu_receive_id":"chat"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got := loadConfigFrom(path)
	if got.FeishuAppID != "app" || got.FeishuReceiveID != "chat" || got.FeishuReceiveType != "chat_id" {
		t.Fatalf("unexpected config: %#v", got)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) == "" || containsJSONKey(encoded, "feishu_app_secret") {
		t.Fatalf("secret field must not be serialized: %s", encoded)
	}
}

func TestSaveConfigUsesPrivatePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix permission bits reliably")
	}
	path := filepath.Join(t.TempDir(), configName)
	c := DefaultConfig()
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
}

func containsJSONKey(data []byte, key string) bool {
	var values map[string]any
	_ = json.Unmarshal(data, &values)
	_, ok := values[key]
	return ok
}
