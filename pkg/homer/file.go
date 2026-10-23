package homer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// newFileMode is used when the generated config does not exist yet (extra pages).
const newFileMode = 0o644

func GetConfig(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	config, err := unmarshalConfig(b)
	if err != nil {
		return Config{}, fmt.Errorf("error unmarshalling config file: %w", err)
	}

	return config, nil
}

func unmarshalConfig(contents []byte) (Config, error) {
	config := Config{}

	if len(contents) == 0 {
		return config, fmt.Errorf("homer config is empty")
	}

	if err := yaml.Unmarshal(contents, &config); err != nil {
		return config, err
	}

	// the footer can be disabled by changing it from a 'string' to a 'bool'
	if config.Footer == "false" {
		config.Footer = false
	}

	return config, nil
}

// PutConfig writes the config to path atomically: the content goes to a
// temporary file in the same directory which is then renamed over path, so
// Homer never reads a half-written file. An existing file keeps its mode and
// owner; a missing one is created.
func PutConfig(config Config, path string) error {
	b, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("error marshalling data into YAML: %w", err)
	}

	mode := os.FileMode(newFileMode)
	fileStats, err := os.Stat(path)
	switch {
	case err == nil:
		mode = fileStats.Mode()
	case errors.Is(err, os.ErrNotExist):
		fileStats = nil
	default:
		return fmt.Errorf("error getting stats for config file '%s': %w", path, err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("error creating temporary file next to '%s': %w", path, err)
	}
	tmpPath := tmp.Name()
	// Clean up on any failure after this point. Rename on success makes this a no-op.
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("error writing data to temporary file '%s': %w", tmpPath, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("error syncing temporary file '%s': %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("error closing temporary file '%s': %w", tmpPath, err)
	}

	if err := os.Chmod(tmpPath, mode); err != nil {
		return fmt.Errorf("error setting mode of '%s': %w", tmpPath, err)
	}
	if fileStats != nil {
		if err := preserveOwner(tmpPath, fileStats); err != nil {
			return err
		}
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("error replacing config file '%s': %w", path, err)
	}

	return nil
}
