// Package strategy provides strategy plugin loading and hot-reload capabilities.
package strategy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"plugin"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// PluginLoader manages dynamic loading, unloading, and reloading of strategy plugins.
// It provides hot-swap capabilities without restarting the main binary.
type PluginLoader struct {
	mu       sync.RWMutex
	plugins  map[string]*PluginInfo
	registry *Registry
	logger   zerolog.Logger
	watchDir string
}

// PluginInfo holds metadata about a loaded plugin.
type PluginInfo struct {
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	LoadedAt    time.Time `json:"loaded_at"`
	LastModTime time.Time `json:"last_mod_time"`
	Size        int64     `json:"size"`
	Version     string    `json:"version"`
	Description string    `json:"description"`
	Status      string    `json:"status"` // active, unloaded, error
	Error       string    `json:"error,omitempty"`
}

// PluginSymbol is the expected exported symbol name in plugin .so files.
const PluginSymbol = "Strategy"

// NewPluginLoader creates a new PluginLoader instance.
func NewPluginLoader(registry *Registry, logger zerolog.Logger) *PluginLoader {
	return &PluginLoader{
		plugins:  make(map[string]*PluginInfo),
		registry: registry,
		logger:   logger.With().Str("component", "plugin_loader").Logger(),
	}
}

// SetWatchDir sets the directory to watch for plugin files.
func (pl *PluginLoader) SetWatchDir(dir string) error {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("watch directory does not exist: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("watch path is not a directory: %s", dir)
	}

	pl.watchDir = dir
	pl.logger.Info().Str("dir", dir).Msg("Plugin watch directory set")
	return nil
}

// WatchDir returns the current watch directory.
func (pl *PluginLoader) WatchDir() string {
	pl.mu.RLock()
	defer pl.mu.RUnlock()
	return pl.watchDir
}

// Load loads a strategy plugin from the given file path.
// The plugin must export a symbol named "Strategy" that implements the Strategy interface.
func (pl *PluginLoader) Load(path string) (*PluginInfo, error) {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve plugin path: %w", err)
	}

	// Check if already loaded
	for _, info := range pl.plugins {
		if info.Path == absPath && info.Status == "active" {
			return nil, fmt.Errorf("plugin already loaded: %s", absPath)
		}
	}

	// Get file info for metadata
	fileInfo, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat plugin file: %w", err)
	}

	// Open the plugin
	plug, err := plugin.Open(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open plugin %s: %w", absPath, err)
	}

	// Look up the Strategy symbol
	symStrategy, err := plug.Lookup(PluginSymbol)
	if err != nil {
		return nil, fmt.Errorf("plugin %s does not export '%s' symbol: %w", absPath, PluginSymbol, err)
	}

	// Type assert to Strategy interface
	strategy, ok := symStrategy.(Strategy)
	if !ok {
		return nil, fmt.Errorf("plugin %s '%s' symbol does not implement Strategy interface", absPath, PluginSymbol)
	}

	// Register with the registry
	if err := pl.registry.Register(strategy); err != nil {
		return nil, fmt.Errorf("failed to register plugin strategy: %w", err)
	}

	// Create plugin info
	info := &PluginInfo{
		Name:        strategy.Name(),
		Path:        absPath,
		LoadedAt:    time.Now().UTC(),
		LastModTime: fileInfo.ModTime().UTC(),
		Size:        fileInfo.Size(),
		Version:     extractVersion(absPath),
		Description: strategy.Description(),
		Status:      "active",
	}

	pl.plugins[info.Name] = info

	pl.logger.Info().
		Str("name", info.Name).
		Str("path", absPath).
		Int64("size", info.Size).
		Msg("Plugin loaded successfully")

	return info, nil
}

// Unload unloads a strategy plugin by name.
// It removes the strategy from the registry and marks the plugin as unloaded.
func (pl *PluginLoader) Unload(name string) error {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	info, exists := pl.plugins[name]
	if !exists {
		return fmt.Errorf("plugin not found: %s", name)
	}

	if info.Status != "active" {
		return fmt.Errorf("plugin %s is not active (status: %s)", name, info.Status)
	}

	// Note: Go plugins cannot be truly unloaded from memory.
	// We remove from our registry and mark as unloaded.
	// The plugin file handle will be released on process restart.
	info.Status = "unloaded"
	info.Error = ""

	pl.logger.Info().Str("name", name).Msg("Plugin unloaded")
	return nil
}

// Reload reloads a strategy plugin by path.
// Since Go does not support unloading .so files, reload requires:
// 1. Mark old plugin as unloaded
// 2. Load new plugin file (must be a different file path or binary)
func (pl *PluginLoader) Reload(path string) (*PluginInfo, error) {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve plugin path: %w", err)
	}

	// Find existing plugin by path
	var existingName string
	for name, info := range pl.plugins {
		if info.Path == absPath {
			existingName = name
			break
		}
	}

	// If plugin exists, mark as unloaded first
	if existingName != "" {
		pl.plugins[existingName].Status = "unloaded"
		pl.logger.Info().Str("name", existingName).Msg("Marked old plugin for reload")
	}

	// Load the new plugin
	// Note: Due to Go plugin limitations, the .so file must be a different
	// file on disk (e.g., copied to a temp location) for true reload.
	// We attempt to load anyway; if it fails, user must use a temp copy.
	pl.mu.Unlock()
	info, err := pl.Load(absPath)
	pl.mu.Lock()

	if err != nil {
		// Restore old status if reload failed
		if existingName != "" {
			pl.plugins[existingName].Status = "active"
		}
		return nil, fmt.Errorf("reload failed: %w", err)
	}

	pl.logger.Info().
		Str("name", info.Name).
		Str("path", absPath).
		Msg("Plugin reloaded successfully")

	return info, nil
}

// ReloadByName reloads a plugin by strategy name.
// It looks up the original path and attempts to reload from there.
func (pl *PluginLoader) ReloadByName(name string) (*PluginInfo, error) {
	pl.mu.RLock()
	info, exists := pl.plugins[name]
	pl.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("plugin not found: %s", name)
	}

	return pl.Reload(info.Path)
}

// List returns information about all loaded plugins.
func (pl *PluginLoader) List() []PluginInfo {
	pl.mu.RLock()
	defer pl.mu.RUnlock()

	result := make([]PluginInfo, 0, len(pl.plugins))
	for _, info := range pl.plugins {
		result = append(result, *info)
	}
	return result
}

// ListActive returns only active plugins.
func (pl *PluginLoader) ListActive() []PluginInfo {
	pl.mu.RLock()
	defer pl.mu.RUnlock()

	result := make([]PluginInfo, 0)
	for _, info := range pl.plugins {
		if info.Status == "active" {
			result = append(result, *info)
		}
	}
	return result
}

// Get returns information about a specific plugin.
func (pl *PluginLoader) Get(name string) (*PluginInfo, error) {
	pl.mu.RLock()
	defer pl.mu.RUnlock()

	info, exists := pl.plugins[name]
	if !exists {
		return nil, fmt.Errorf("plugin not found: %s", name)
	}
	// Return a copy
	copy := *info
	return &copy, nil
}

// LoadAll loads all .so plugin files from the watch directory.
func (pl *PluginLoader) LoadAll() ([]PluginInfo, []error) {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	if pl.watchDir == "" {
		return nil, []error{fmt.Errorf("watch directory not set")}
	}

	entries, err := os.ReadDir(pl.watchDir)
	if err != nil {
		return nil, []error{fmt.Errorf("failed to read watch directory: %w", err)}
	}

	var loaded []PluginInfo
	var errs []error

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".so") {
			continue
		}

		path := filepath.Join(pl.watchDir, entry.Name())
		pl.mu.Unlock()
		info, err := pl.Load(path)
		pl.mu.Lock()

		if err != nil {
			errs = append(errs, fmt.Errorf("failed to load %s: %w", entry.Name(), err))
			continue
		}
		loaded = append(loaded, *info)
	}

	return loaded, errs
}

// Watch starts watching the watch directory for changes.
// It periodically checks for new or modified .so files and loads them.
// Call StopWatch to stop watching.
func (pl *PluginLoader) Watch(ctx context.Context, interval time.Duration) {
	if pl.watchDir == "" {
		pl.logger.Warn().Msg("Cannot start watch: watch directory not set")
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	pl.logger.Info().
		Str("dir", pl.watchDir).
		Dur("interval", interval).
		Msg("Started plugin directory watch")

	for {
		select {
		case <-ctx.Done():
			pl.logger.Info().Msg("Stopped plugin directory watch")
			return
		case <-ticker.C:
			pl.checkAndReload()
		}
	}
}

// checkAndReload scans the watch directory and reloads modified plugins.
func (pl *PluginLoader) checkAndReload() {
	pl.mu.RLock()
	watchDir := pl.watchDir
	pl.mu.RUnlock()

	if watchDir == "" {
		return
	}

	entries, err := os.ReadDir(watchDir)
	if err != nil {
		pl.logger.Error().Err(err).Str("dir", watchDir).Msg("Failed to read watch directory")
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".so") {
			continue
		}

		path := filepath.Join(watchDir, entry.Name())
		info, err := os.Stat(path)
		if err != nil {
			continue
		}

		pl.mu.RLock()
		var existing *PluginInfo
		for _, p := range pl.plugins {
			if p.Path == path && p.Status == "active" {
				existing = p
				break
			}
		}
		pl.mu.RUnlock()

		if existing == nil {
			// New plugin detected
			pl.logger.Info().Str("file", entry.Name()).Msg("New plugin detected, auto-loading")
			if _, err := pl.Load(path); err != nil {
				pl.logger.Error().Err(err).Str("file", entry.Name()).Msg("Auto-load failed")
			}
		} else if info.ModTime().After(existing.LastModTime) {
			// Modified plugin detected
			pl.logger.Info().Str("file", entry.Name()).Msg("Plugin modification detected, auto-reloading")
			if _, err := pl.Reload(path); err != nil {
				pl.logger.Error().Err(err).Str("file", entry.Name()).Msg("Auto-reload failed")
			}
		}
	}
}

// extractVersion attempts to extract a version from the plugin filename.
// Expected format: strategyname_v1.2.3.so
func extractVersion(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, ".so")

	// Look for _v pattern
	idx := strings.LastIndex(base, "_v")
	if idx != -1 && idx+2 < len(base) {
		version := base[idx+2:]
		// Basic validation: version should contain digits
		for _, c := range version {
			if c >= '0' && c <= '9' {
				return version
			}
		}
	}

	return "unknown"
}

// SetPluginForTesting sets a plugin info for testing purposes.
// This should only be used in tests.
func (pl *PluginLoader) SetPluginForTesting(name string, info *PluginInfo) {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	pl.plugins[name] = info
}

// GlobalPluginLoader is the default plugin loader instance.
var GlobalPluginLoader *PluginLoader

// InitPluginLoader initializes the global plugin loader.
func InitPluginLoader(registry *Registry, logger zerolog.Logger) {
	GlobalPluginLoader = NewPluginLoader(registry, logger)
}
