package orchestrator

import (
	"fmt"
	"os"
	"strings"

	"github.com/ilyaus/loomwork/internal/model"
)

// MaxReferenceBytes bounds how much of a referenced file is loaded as context.
const MaxReferenceBytes = 1 << 20 // 1 MiB

// ArtifactContent returns the text of an artifact: inline content directly, or
// the contents of a local file reference. Remote references are rejected rather
// than silently fetched, so a prompt run never makes an unexpected network call.
func ArtifactContent(artifact model.Artifact) (string, error) {
	if artifact.Body.Content != "" {
		return artifact.Body.Content, nil
	}
	ref := strings.TrimSpace(artifact.Body.Ref)
	if ref == "" {
		return "", fmt.Errorf("artifact %q (v%d) has neither content nor reference", artifact.Name, artifact.Version)
	}
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return "", fmt.Errorf("artifact %q references %s: remote references are not fetched by the foundation; add it as inline content instead", artifact.Name, ref)
	}
	info, err := os.Stat(ref)
	if err != nil {
		return "", fmt.Errorf("artifact %q reference %s: %w", artifact.Name, ref, err)
	}
	if info.Size() > MaxReferenceBytes {
		return "", fmt.Errorf("artifact %q reference %s is %d bytes, exceeding the %d byte context limit", artifact.Name, ref, info.Size(), MaxReferenceBytes)
	}
	raw, err := os.ReadFile(ref)
	if err != nil {
		return "", fmt.Errorf("read artifact %q reference %s: %w", artifact.Name, ref, err)
	}
	return string(raw), nil
}
