package recommendation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const EngineManifestVersion = "isolated-engine-manifest-v0.1"
const maximumEngineManifestBytes = 1 << 20

var manifestSHA256Pattern = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

type engineManifest struct {
	Version   string           `json:"version"`
	Artifacts []EngineArtifact `json:"artifacts"`
}

// LoadEngineArtifactManifest turns a reviewed on-disk manifest into the policy
// consumed by NewSubprocessRunner. Worker files must stay inside workDir; the
// interpreter executable is the only explicitly permitted external artifact.
func LoadEngineArtifactManifest(path, workDir, executable string) ([]EngineArtifact, error) {
	for name, value := range map[string]string{"manifest": path, "working directory": workDir, "executable": executable} {
		if !filepath.IsAbs(value) {
			return nil, fmt.Errorf("engine %s path must be absolute", name)
		}
	}
	content, err := readLimitedFile(path, maximumEngineManifestBytes)
	if err != nil {
		return nil, fmt.Errorf("read engine manifest: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var manifest engineManifest
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode engine manifest: %w", err)
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return nil, err
	}
	if manifest.Version != EngineManifestVersion || len(manifest.Artifacts) == 0 {
		return nil, errors.New("engine manifest has unsupported version or no artifacts")
	}
	cleanWorkDir, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		return nil, fmt.Errorf("resolve engine working directory: %w", err)
	}
	cleanExecutable, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return nil, fmt.Errorf("resolve engine executable: %w", err)
	}
	result := make([]EngineArtifact, 0, len(manifest.Artifacts))
	seen := make(map[string]bool, len(manifest.Artifacts))
	executableCovered := false
	for index, artifact := range manifest.Artifacts {
		if !filepath.IsAbs(artifact.Path) || !manifestSHA256Pattern.MatchString(artifact.SHA256) {
			return nil, fmt.Errorf("engine manifest artifact %d has invalid path or SHA-256", index)
		}
		resolved, err := filepath.EvalSymlinks(artifact.Path)
		if err != nil {
			return nil, fmt.Errorf("resolve engine manifest artifact %d: %w", index, err)
		}
		if seen[resolved] {
			return nil, fmt.Errorf("engine manifest repeats artifact %q", resolved)
		}
		seen[resolved] = true
		if resolved == cleanExecutable {
			executableCovered = true
		} else if !pathWithin(cleanWorkDir, resolved) {
			return nil, fmt.Errorf("engine artifact %q is outside the approved working directory", resolved)
		}
		result = append(result, EngineArtifact{Path: resolved, SHA256: strings.ToLower(artifact.SHA256)})
	}
	if !executableCovered {
		return nil, errors.New("engine manifest does not cover configured executable")
	}
	return result, nil
}

func readLimitedFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > limit {
		return nil, errors.New("file exceeds size limit")
	}
	return content, nil
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("engine manifest contains trailing JSON")
		}
		return fmt.Errorf("decode trailing engine manifest content: %w", err)
	}
	return nil
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
