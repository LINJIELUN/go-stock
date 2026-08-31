package recommendation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func manifestFixture(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	worker := filepath.Join(root, "worker.py")
	executable := filepath.Join(root, "python")
	if err := os.WriteFile(worker, []byte("worker"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("python"), 0o700); err != nil {
		t.Fatal(err)
	}
	hash := func(path string) string {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(content)
		return hex.EncodeToString(digest[:])
	}
	manifestPath := filepath.Join(root, "manifest.json")
	payload, _ := json.Marshal(engineManifest{Version: EngineManifestVersion, Artifacts: []EngineArtifact{{Path: executable, SHA256: hash(executable)}, {Path: worker, SHA256: hash(worker)}}})
	if err := os.WriteFile(manifestPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	return manifestPath, root, executable
}

func TestLoadEngineArtifactManifest(t *testing.T) {
	manifest, root, executable := manifestFixture(t)
	artifacts, err := LoadEngineArtifactManifest(manifest, root, executable)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 2 || artifacts[0].Path != executable {
		t.Fatalf("unexpected artifacts: %+v", artifacts)
	}
}

func TestEngineManifestRejectsUnknownAndTrailingContent(t *testing.T) {
	manifest, root, executable := manifestFixture(t)
	content, _ := os.ReadFile(manifest)
	if err := os.WriteFile(manifest, append(content, []byte(` {}`)...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadEngineArtifactManifest(manifest, root, executable); err == nil {
		t.Fatal("expected trailing JSON rejection")
	}
	if err := os.WriteFile(manifest, []byte(`{"version":"isolated-engine-manifest-v0.1","artifacts":[],"secret":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadEngineArtifactManifest(manifest, root, executable); err == nil {
		t.Fatal("expected unknown field rejection")
	}
}

func TestEngineManifestRejectsOutsideWorkerDirectory(t *testing.T) {
	manifest, root, executable := manifestFixture(t)
	outside := filepath.Join(t.TempDir(), "worker.py")
	if err := os.WriteFile(outside, []byte("worker"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("worker"))
	executableDigest := sha256.Sum256([]byte("python"))
	payload, _ := json.Marshal(engineManifest{Version: EngineManifestVersion, Artifacts: []EngineArtifact{{Path: executable, SHA256: hex.EncodeToString(executableDigest[:])}, {Path: outside, SHA256: hex.EncodeToString(digest[:])}}})
	if err := os.WriteFile(manifest, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadEngineArtifactManifest(manifest, root, executable); err == nil {
		t.Fatal("expected outside artifact rejection")
	}
}
