package reviewloop

import (
	"fmt"
	"os"
	"path/filepath"
)

const runsRootDir = "log/runs"

type artifactPaths struct {
	runDir            string
	capturesDir       string
	metadataPath      string
	transitionsPath   string
	channelEventsPath string
	resultPath        string
}

func newArtifactPaths(rootDir string, runID string) (artifactPaths, error) {
	runDir := filepath.Join(rootDir, runID)
	capturesDir := filepath.Join(runDir, "captures")
	if err := os.MkdirAll(capturesDir, 0o755); err != nil {
		return artifactPaths{}, fmt.Errorf("failed to create artifact directory %s: %w", runDir, err)
	}
	return artifactPaths{
		runDir:            runDir,
		capturesDir:       capturesDir,
		metadataPath:      filepath.Join(runDir, "metadata.json"),
		transitionsPath:   filepath.Join(runDir, "state-transitions.jsonl"),
		channelEventsPath: filepath.Join(runDir, "channel-events.jsonl"),
		resultPath:        filepath.Join(runDir, "result.json"),
	}, nil
}

func successCaptureName(iteration int, role string) string {
	return fmt.Sprintf("iter-%d-%s.txt", iteration, role)
}

func failureCaptureName(iteration int, role string, suffix string) string {
	return fmt.Sprintf("iter-%d-%s-%s.txt", iteration, role, suffix)
}
