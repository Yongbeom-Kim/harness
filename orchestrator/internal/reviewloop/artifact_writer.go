package reviewloop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type artifactWriter struct {
	paths artifactPaths
}

func NewArtifactWriter(runID string) (ArtifactSink, error) {
	paths, err := newArtifactPaths(runsRootDir, runID)
	if err != nil {
		return nil, err
	}
	return &artifactWriter{paths: paths}, nil
}

func (w *artifactWriter) WriteMetadata(metadata RunMetadata) error {
	return writeJSONFile(w.paths.metadataPath, metadata)
}

func (w *artifactWriter) AppendTransition(transition StateTransition) error {
	return appendJSONL(w.paths.transitionsPath, "transition log", transition)
}

func (w *artifactWriter) AppendChannelEvent(event ChannelEvent) error {
	return appendJSONL(w.paths.channelEventsPath, "channel event log", event)
}

func (w *artifactWriter) WriteCapture(name string, text string) error {
	path := filepath.Join(w.paths.capturesDir, name)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return fmt.Errorf("failed to write capture %s: %w", name, err)
	}
	return nil
}

func (w *artifactWriter) WriteResult(result RunResult) error {
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

func appendJSONL(path string, description string, value any) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", description, err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("failed to append %s: %w", description, err)
	}
	return nil
}
