package main

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ruoxizhnya/quant-trading/docs"
)

// swaggerUIHTML is the minimal Swagger UI page that loads the
// swagger-ui-dist assets from the jsdelivr CDN and points them at
// the /api/openapi.yaml spec endpoint. Embedding the HTML as a
// string constant keeps the handler self-contained — no extra
// static files are needed.
const swaggerUIHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Quant Lab API Documentation</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css">
  <style>
    body { margin: 0; }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.onload = function() {
      window.ui = SwaggerUIBundle({
        url: "/api/openapi.yaml",
        dom_id: "#swagger-ui",
        deepLinking: true,
        presets: [SwaggerUIBundle.presets.apis],
        layout: "BaseLayout"
      });
    };
  </script>
</body>
</html>`

// registerOpenAPIRoutes wires the OpenAPI spec and Swagger UI endpoints.
//
//   - GET /api/openapi.yaml — serves the embedded OpenAPI 3.0 YAML spec.
//   - GET /api/docs        — serves the Swagger UI HTML page (loads the
//     spec from /api/openapi.yaml via CDN-hosted
//     swagger-ui-dist).
func registerOpenAPIRoutes(router *gin.Engine) {
	router.GET("/api/openapi.yaml", serveOpenAPISpec)
	router.GET("/api/docs", serveSwaggerUI)
}

// serveOpenAPISpec returns the embedded OpenAPI 3.0 YAML with the
// correct content type so browsers and tools recognise it as YAML.
func serveOpenAPISpec(c *gin.Context) {
	c.Header("Content-Disposition", "inline; filename=openapi.yaml")
	c.Data(http.StatusOK, "application/yaml; charset=utf-8", docs.OpenAPISpec)
}

// serveSwaggerUI returns the Swagger UI HTML page. The page loads
// swagger-ui-dist from the jsdelivr CDN and fetches the spec from
// /api/openapi.yaml.
func serveSwaggerUI(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(swaggerUIHTML))
}
