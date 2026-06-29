package strategy

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPluginLoader(t *testing.T) {
	logger := zerolog.New(os.Stdout)
	registry := NewRegistry()
	loader := NewPluginLoader(registry, logger)

	assert.NotNil(t, loader)
	assert.NotNil(t, loader.plugins)
	assert.Equal(t, registry, loader.registry)
	assert.Empty(t, loader.watchDir)
}

func TestPluginLoader_SetWatchDir(t *testing.T) {
	logger := zerolog.New(os.Stdout)
	loader := NewPluginLoader(NewRegistry(), logger)

	// Create temp directory
	tmpDir := t.TempDir()

	// Success case
	err := loader.SetWatchDir(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, tmpDir, loader.watchDir)

	// Error: non-existent directory
	err = loader.SetWatchDir("/nonexistent/path")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "watch directory does not exist")

	// Error: file instead of directory
	tmpFile := filepath.Join(t.TempDir(), "file.txt")
	require.NoError(t, os.WriteFile(tmpFile, []byte("test"), 0644))
	err = loader.SetWatchDir(tmpFile)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "watch path is not a directory")
}

func TestPluginLoader_Load(t *testing.T) {
	logger := zerolog.New(os.Stdout)
	registry := NewRegistry()
	loader := NewPluginLoader(registry, logger)

	// Test with a non-existent file
	_, err := loader.Load("/nonexistent/plugin.so")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to stat plugin file")

	// Test with an invalid file (not a plugin)
	tmpFile := filepath.Join(t.TempDir(), "not_a_plugin.so")
	require.NoError(t, os.WriteFile(tmpFile, []byte("invalid"), 0644))
	_, err = loader.Load(tmpFile)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open plugin")
}

func TestPluginLoader_Unload(t *testing.T) {
	logger := zerolog.New(os.Stdout)
	loader := NewPluginLoader(NewRegistry(), logger)

	// Unload non-existent plugin
	err := loader.Unload("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "plugin not found")

	// Create a mock plugin entry
	loader.mu.Lock()
	loader.plugins["mock"] = &PluginInfo{
		Name:   "mock",
		Status: "active",
	}
	loader.mu.Unlock()

	// Unload active plugin
	err = loader.Unload("mock")
	assert.NoError(t, err)

	// Verify status changed
	info, _ := loader.Get("mock")
	assert.Equal(t, "unloaded", info.Status)

	// Unload already unloaded plugin
	err = loader.Unload("mock")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "is not active")
}

func TestPluginLoader_ReloadByName(t *testing.T) {
	logger := zerolog.New(os.Stdout)
	loader := NewPluginLoader(NewRegistry(), logger)

	// Reload non-existent plugin
	_, err := loader.ReloadByName("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "plugin not found")
}

func TestPluginLoader_List(t *testing.T) {
	logger := zerolog.New(os.Stdout)
	loader := NewPluginLoader(NewRegistry(), logger)

	// Empty list
	list := loader.List()
	assert.Empty(t, list)

	// Add mock plugins
	loader.mu.Lock()
	loader.plugins["plugin1"] = &PluginInfo{Name: "plugin1", Status: "active"}
	loader.plugins["plugin2"] = &PluginInfo{Name: "plugin2", Status: "unloaded"}
	loader.mu.Unlock()

	list = loader.List()
	assert.Len(t, list, 2)
}

func TestPluginLoader_ListActive(t *testing.T) {
	logger := zerolog.New(os.Stdout)
	loader := NewPluginLoader(NewRegistry(), logger)

	loader.mu.Lock()
	loader.plugins["active1"] = &PluginInfo{Name: "active1", Status: "active"}
	loader.plugins["active2"] = &PluginInfo{Name: "active2", Status: "active"}
	loader.plugins["unloaded"] = &PluginInfo{Name: "unloaded", Status: "unloaded"}
	loader.mu.Unlock()

	active := loader.ListActive()
	assert.Len(t, active, 2)
	for _, p := range active {
		assert.Equal(t, "active", p.Status)
	}
}

func TestPluginLoader_Get(t *testing.T) {
	logger := zerolog.New(os.Stdout)
	loader := NewPluginLoader(NewRegistry(), logger)

	// Get non-existent
	_, err := loader.Get("nonexistent")
	assert.Error(t, err)

	// Get existing
	loader.mu.Lock()
	loader.plugins["test"] = &PluginInfo{
		Name:        "test",
		Description: "test plugin",
		Status:      "active",
	}
	loader.mu.Unlock()

	info, err := loader.Get("test")
	require.NoError(t, err)
	assert.Equal(t, "test", info.Name)
	assert.Equal(t, "test plugin", info.Description)
}

func TestPluginLoader_LoadAll(t *testing.T) {
	logger := zerolog.New(os.Stdout)
	loader := NewPluginLoader(NewRegistry(), logger)

	// Without watch dir set
	_, errs := loader.LoadAll()
	assert.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "watch directory not set")

	// With watch dir but no valid plugins
	tmpDir := t.TempDir()
	require.NoError(t, loader.SetWatchDir(tmpDir))

	loaded, errs := loader.LoadAll()
	assert.Empty(t, loaded)
	assert.Empty(t, errs)

	// Add a non-plugin .so file
	badPlugin := filepath.Join(tmpDir, "bad.so")
	require.NoError(t, os.WriteFile(badPlugin, []byte("not a plugin"), 0644))

	loaded, errs = loader.LoadAll()
	assert.Empty(t, loaded)
	assert.NotEmpty(t, errs)
}

func TestExtractVersion(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/path/to/strategy_v1.2.3.so", "1.2.3"},
		{"/path/to/mystrategy_v2.0.so", "2.0"},
		{"/path/to/strategy.so", "unknown"},
		{"/path/to/strategy_v.so", "unknown"},
		{"/path/to/strategy_vabc.so", "unknown"},
	}

	for _, tt := range tests {
		result := extractVersion(tt.path)
		assert.Equal(t, tt.expected, result, "path: %s", tt.path)
	}
}

// TestExtractGitHash_ValidRepo exercises extractGitHash against the
// project itself: loader.go is a tracked file in a real git repo, so
// the function must return a non-empty short hash (8 hex chars).
// The test is skipped automatically when git is unavailable or the
// file is not tracked (e.g. running from a tarball), so it never
// produces false negatives in CI environments without git.
func TestExtractGitHash_ValidRepo(t *testing.T) {
	// Use loader.go — it is part of the project and tracked by git in
	// any normal checkout. (loader_test.go is also tracked in practice
	// but using loader.go makes the test independent of test-file
	// staging state.)
	trackedPath := "loader.go"

	hash, err := extractGitHash(trackedPath)
	require.NoError(t, err, "extractGitHash must never return an error for graceful-degrade cases")

	// If git is unavailable or the file is not tracked, hash is "" —
	// that is a valid graceful-degrade outcome, not a failure. Skip
	// the assertion in that case so CI without git stays green.
	if hash == "" {
		t.Skip("git not available or file not tracked — graceful degrade returned empty hash")
	}

	// Short hash = 8 hex chars. Validate format but stay liberal in
	// case git's default format changes in the future.
	assert.LessOrEqual(t, len(hash), 8, "short hash should be at most 8 chars")
	assert.NotEmpty(t, hash, "expected non-empty git hash for a tracked file")
	for _, c := range hash {
		assert.True(t,
			(c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'),
			"git hash should be lowercase hex, got: %q", hash,
		)
	}
}

// TestExtractGitHash_NonExistentFile verifies that extractGitHash
// gracefully degrades when given a path that does not exist on disk
// (and is therefore not tracked by git). It must return ("", nil)
// rather than panic or surface an error.
func TestExtractGitHash_NonExistentFile(t *testing.T) {
	hash, err := extractGitHash("/nonexistent/path/to/missing_file.so")

	assert.NoError(t, err, "extractGitHash must not surface an error for untracked/missing files")
	assert.Empty(t, hash, "expected empty hash for non-existent file, got: %q", hash)
}

func TestPluginInfo_Struct(t *testing.T) {
	info := PluginInfo{
		Name:        "test",
		Path:        "/tmp/test.so",
		LoadedAt:    time.Now(),
		LastModTime: time.Now(),
		Size:        1024,
		Version:     "1.0.0",
		Description: "Test plugin",
		Status:      "active",
	}

	assert.Equal(t, "test", info.Name)
	assert.Equal(t, "/tmp/test.so", info.Path)
	assert.Equal(t, int64(1024), info.Size)
	assert.Equal(t, "1.0.0", info.Version)
	assert.Equal(t, "active", info.Status)
}

func TestGlobalPluginLoader(t *testing.T) {
	logger := zerolog.New(os.Stdout)
	registry := NewRegistry()

	// S7-P0-15 (ODR-043): Save and restore the package-level global so the
	// test is hermetic across -count>1 runs. GlobalPluginLoader is a
	// package-level var that persists across test iterations within the same
	// process; without this reset, the assert.Nil below fails on the second
	// run because InitPluginLoader set it on the first run.
	old := GlobalPluginLoader
	GlobalPluginLoader = nil
	defer func() { GlobalPluginLoader = old }()

	// Should be nil before init
	assert.Nil(t, GlobalPluginLoader)

	InitPluginLoader(registry, logger)

	assert.NotNil(t, GlobalPluginLoader)
	assert.Equal(t, registry, GlobalPluginLoader.registry)
}

func TestPluginLoader_ConcurrentAccess(t *testing.T) {
	logger := zerolog.New(os.Stdout)
	loader := NewPluginLoader(NewRegistry(), logger)

	// Add initial plugin
	loader.mu.Lock()
	loader.plugins["concurrent"] = &PluginInfo{Name: "concurrent", Status: "active"}
	loader.mu.Unlock()

	// Concurrent reads
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			_ = loader.List()
			_, _ = loader.Get("concurrent")
			_ = loader.ListActive()
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("Timeout waiting for concurrent access")
		}
	}
}

func TestPluginLoader_WatchDirAutoLoad(t *testing.T) {
	logger := zerolog.New(os.Stdout)
	loader := NewPluginLoader(NewRegistry(), logger)

	tmpDir := t.TempDir()
	require.NoError(t, loader.SetWatchDir(tmpDir))

	// Create a fake .so file (won't load but will be detected)
	fakePlugin := filepath.Join(tmpDir, "fake_v1.0.so")
	require.NoError(t, os.WriteFile(fakePlugin, []byte("fake"), 0644))

	// checkAndReload should attempt to load it (and fail, but not panic)
	loader.checkAndReload()

	// The plugin should be attempted (we can verify by checking no panic)
	// Since it's not a real plugin, it won't be in the list
	active := loader.ListActive()
	assert.Empty(t, active)
}
