package web

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed static
var staticFiles embed.FS

// ServeUI serves files from the embedded static/ directory under the /ui/ prefix.
// Unknown paths fall back to index.html so the SPA router can take over.
func ServeUI(c *gin.Context) {
	subFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	// Strip the /ui prefix to get the file path within the embedded FS.
	rel := strings.TrimPrefix(c.Request.URL.Path, "/ui")
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		rel = "index.html"
	}

	// Fall back to index.html when the path doesn't match an embedded file.
	if _, err := fs.Stat(subFS, rel); err != nil {
		rel = "index.html"
	}

	data, err := fs.ReadFile(subFS, rel)
	if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	ct := mime.TypeByExtension(filepath.Ext(rel))
	if ct == "" {
		ct = http.DetectContentType(data)
	}
	c.Data(http.StatusOK, ct, data)
}
