package api

import (
	"gubexplorer/internal/k8s"
	"gubexplorer/web"
	"net/http"

	"github.com/gin-gonic/gin"
)

// NewRouter creates and configures the gin router.
// authUser/authPass are mandatory — NewRouter panics if either is empty.
// version is the application version string injected via ldflags.
func NewRouter(client *k8s.Client, defaultNamespace, authUser, authPass, version string) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	// Basic Auth – applied globally before any route, so it covers UI + API.
	if authUser != "" && authPass != "" {
		r.Use(gin.BasicAuth(gin.Accounts{authUser: authPass}))
	}

	h := &Handler{client: client, defaultNS: defaultNamespace, version: version}

	// Serve embedded static files
	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/ui/")
	})
	r.GET("/ui/*path", web.ServeUI)

	v1 := r.Group("/api/v1")
	{
		v1.GET("/config", h.GetConfig)
		v1.GET("/namespaces", h.ListNamespaces)
		v1.GET("/resource-types", h.ListResourceTypes)
		// Permissions – static prefix prevents conflict with /:namespace param routes
		v1.GET("/permissions/:namespace", h.GetPermissions)
		// CRD discovery
		v1.GET("/crd-types", h.ListCRDTypes)

		// Resources
		v1.GET("/:namespace/:resource", h.ListResources)
		v1.GET("/:namespace/:resource/:name", h.GetResource)
		v1.PUT("/:namespace/:resource/:name", h.UpdateResource)
		v1.DELETE("/:namespace/:resource/:name", h.DeleteResource)

		// Actions
		v1.POST("/:namespace/:resource/:name/scale", h.ScaleResource)
		v1.POST("/:namespace/:resource/:name/restart", h.RestartResource)

		// Pod-specific
		v1.GET("/:namespace/pods/:name/containers", h.GetPodContainers)
		v1.GET("/:namespace/pods/:name/logs", h.GetPodLogs)
		v1.GET("/:namespace/pods/:name/logs/stream", h.StreamPodLogs)
	}

	return r
}
