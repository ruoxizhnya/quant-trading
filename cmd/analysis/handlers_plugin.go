package main

import (
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// registerPluginRoutes registers plugin management API routes.
func registerPluginRoutes(router *gin.Engine, loader *strategy.PluginLoader) {
	if loader == nil {
		return
	}

	// List all plugins
	router.GET("/api/plugins", func(c *gin.Context) {
		plugins := loader.List()
		c.JSON(http.StatusOK, gin.H{
			"plugins": plugins,
			"count":   len(plugins),
		})
	})

	// List active plugins
	router.GET("/api/plugins/active", func(c *gin.Context) {
		plugins := loader.ListActive()
		c.JSON(http.StatusOK, gin.H{
			"plugins": plugins,
			"count":   len(plugins),
		})
	})

	// Get specific plugin info
	router.GET("/api/plugins/:name", func(c *gin.Context) {
		name := c.Param("name")
		info, err := loader.Get(name)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, info)
	})

	// Load a plugin from file path
	router.POST("/api/plugins/load", func(c *gin.Context) {
		var req struct {
			Path string `json:"path" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		info, err := loader.Load(req.Path)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"message": "plugin loaded",
			"plugin":  info,
		})
	})

	// Unload a plugin by name
	router.POST("/api/plugins/:name/unload", func(c *gin.Context) {
		name := c.Param("name")
		if err := loader.Unload(name); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "plugin unloaded", "name": name})
	})

	// Reload a plugin by name
	router.POST("/api/plugins/:name/reload", func(c *gin.Context) {
		name := c.Param("name")
		info, err := loader.ReloadByName(name)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"message": "plugin reloaded",
			"plugin":  info,
		})
	})

	// Reload a plugin from file path
	router.POST("/api/plugins/reload", func(c *gin.Context) {
		var req struct {
			Path string `json:"path" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		info, err := loader.Reload(req.Path)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"message": "plugin reloaded",
			"plugin":  info,
		})
	})

	// Load all plugins from watch directory
	router.POST("/api/plugins/load-all", func(c *gin.Context) {
		loaded, errs := loader.LoadAll()
		var errStrings []string
		for _, e := range errs {
			errStrings = append(errStrings, e.Error())
		}
		c.JSON(http.StatusOK, gin.H{
			"message":  "load all completed",
			"loaded":   loaded,
			"errors":   errStrings,
			"count":    len(loaded),
			"errCount": len(errs),
		})
	})

	// Set watch directory
	router.POST("/api/plugins/watch-dir", func(c *gin.Context) {
		var req struct {
			Dir string `json:"dir" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Resolve to absolute path
		absDir, err := filepath.Abs(req.Dir)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := loader.SetWatchDir(absDir); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"message": "watch directory set",
			"dir":     absDir,
		})
	})

	// Get watch directory
	router.GET("/api/plugins/watch-dir", func(c *gin.Context) {
		// Get watch dir from loader (we need to add a getter)
		c.JSON(http.StatusOK, gin.H{
			"watch_dir": getWatchDir(loader),
		})
	})
}

// getWatchDir extracts the watch directory from a PluginLoader.
func getWatchDir(loader *strategy.PluginLoader) string {
	return loader.WatchDir()
}
