package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
)

func registerProxyRoutes(router *gin.Engine, httpClient *http.Client, _ zerolog.Logger) {
	dataServiceURL := viper.GetString("data_service.url")
	if dataServiceURL == "" {
		dataServiceURL = "http://data-service:8081"
	}

	proxyRequest := func(c *gin.Context, method, targetURL string, body io.Reader) {
		var resp *http.Response
		var err error
		switch method {
		case http.MethodGet:
			resp, err = httpClient.Get(targetURL)
		case http.MethodPost:
			resp, err = httpClient.Post(targetURL, "application/json", body)
		default:
			req, reqErr := http.NewRequestWithContext(c.Request.Context(), method, targetURL, body)
			if reqErr != nil {
				c.JSON(http.StatusBadGateway, gin.H{"error": "failed to create proxy request"})
				return
			}
			if body != nil {
				req.Header.Set("Content-Type", "application/json")
			}
			resp, err = httpClient.Do(req)
		}
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "data service unavailable: " + err.Error()})
			return
		}
		defer resp.Body.Close()
		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "invalid response from data service"})
			return
		}
		c.JSON(resp.StatusCode, result)
	}

	router.GET("/api/ohlcv/:symbol", func(c *gin.Context) {
		symbol := c.Param("symbol")
		startDate := c.Query("start_date")
		endDate := c.Query("end_date")
		if startDate == "" || endDate == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "start_date and end_date required (YYYYMMDD)"})
			return
		}
		dataURL := fmt.Sprintf("%s/ohlcv/%s?start_date=%s&end_date=%s", dataServiceURL, symbol, startDate, endDate)
		proxyRequest(c, http.MethodGet, dataURL, nil)
	})

	router.POST("/api/screen", func(c *gin.Context) {
		var reqBody map[string]interface{}
		if err := json.NewDecoder(c.Request.Body).Decode(&reqBody); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}
		bodyBytes, _ := json.Marshal(reqBody)
		proxyRequest(c, http.MethodPost, dataServiceURL+"/screen", bytes.NewReader(bodyBytes))
	})

	router.GET("/api/stocks/count", func(c *gin.Context) {
		proxyRequest(c, http.MethodGet, dataServiceURL+"/stocks/count", nil)
	})

	router.GET("/api/market/index", func(c *gin.Context) {
		proxyRequest(c, http.MethodGet, dataServiceURL+"/market/index", nil)
	})

	router.POST("/api/sync/calendar", func(c *gin.Context) {
		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
			return
		}
		proxyRequest(c, http.MethodPost, dataServiceURL+"/sync/calendar", bytes.NewReader(bodyBytes))
	})

	router.GET("/ohlcv/:symbol", func(c *gin.Context) {
		c.Request.URL.Path = "/api/ohlcv/" + c.Param("symbol")
		router.HandleContext(c)
	})
	router.POST("/screen", func(c *gin.Context) {
		c.Request.URL.Path = "/api/screen"
		router.HandleContext(c)
	})
	router.GET("/stocks/count", func(c *gin.Context) {
		c.Request.URL.Path = "/api/stocks/count"
		router.HandleContext(c)
	})
	router.GET("/market/index", func(c *gin.Context) {
		c.Request.URL.Path = "/api/market/index"
		router.HandleContext(c)
	})
	router.POST("/sync/calendar", func(c *gin.Context) {
		c.Request.URL.Path = "/api/sync/calendar"
		router.HandleContext(c)
	})

	router.GET("/api/v1/trading/calendar", func(c *gin.Context) {
		start := c.Query("start")
		end := c.Query("end")
		dataURL := fmt.Sprintf("%s/api/v1/trading/calendar?start=%s&end=%s", dataServiceURL, start, end)
		proxyRequest(c, http.MethodGet, dataURL, nil)
	})
}
