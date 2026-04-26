package implementwithreviewer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type ArtifactWriter struct {
	paths artifactPaths
}

func NewArtifactWriter(runID string) (ArtifactSink, error) {
	paths, err := newArtifactPaths(runsRootDir, runID)
	if err != nil {
		return nil, err
	}
	return &ArtifactWriter{paths: paths}, nil
}

func (w *ArtifactWriter) WriteMetadata(metadata RunMetadata) error {
	return writeJSONFile(w.paths.metadataPath, metadata)
}

func (w *ArtifactWriter) AppendTransition(transition StateTransition) error {
	file, err := os.OpenFile(w.paths.transitionsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open transition log: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(transition); err != nil {
		return fmt.Errorf("failed to append transition: %w", err)
	}
	return nil
}

func (w *ArtifactWriter) WriteCapture(name string, text string) error {
	path := filepath.Join(w.paths.capturesDir, name)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return fmt.Errorf("failed to write capture %s: %w", name, err)
	}
	return nil
}

func (w *ArtifactWriter) WriteResult(result RunResult) error {
	return writeJSONFile(w.paths.resultPath, result)
}

func writeJSONFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode JSON for %s: %w", path, err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	return nil
}
