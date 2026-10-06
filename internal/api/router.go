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
func NewRouter(client *k8s.Client, defaultNamespace, saNamespace, serviceAccount, authUser, authPass, version string) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	h := &Handler{client: client, defaultNS: defaultNamespace, saNamespace: saNamespace, serviceAccount: serviceAccount, version: version}

	// Health/probe endpoint — registered BEFORE BasicAuth so Kubernetes probes bypass auth.
	r.GET("/healthz", h.Healthz)

	// Basic Auth — applied to all routes registered after this point (UI + API).
	if authUser != "" && authPass != "" {
		r.Use(gin.BasicAuth(gin.Accounts{authUser: authPass}))
	}

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

		// Pod-specific 4-segment routes — registering these creates a static "pods"
		// node at depth 2 that takes priority over the :resource wildcard. We must
		// therefore also register the 3-segment pod routes explicitly so that
		// GET/PUT/DELETE /ns/pods/:name are routed to the generic CRUD handlers
		// instead of returning gin's default "404 page not found" (plain text).
		// injectPods adds the missing :resource="pods" param before the handler runs.
		injectPods := func(c *gin.Context) {
			c.Params = append(c.Params, gin.Param{Key: "resource", Value: "pods"})
			c.Next()
		}
		v1.GET("/:namespace/pods/:name", injectPods, h.GetResource)
		v1.PUT("/:namespace/pods/:name", injectPods, h.UpdateResource)
		v1.DELETE("/:namespace/pods/:name", injectPods, h.DeleteResource)

		v1.GET("/:namespace/pods/:name/containers", h.GetPodContainers)
		v1.GET("/:namespace/pods/:name/logs", h.GetPodLogs)
		v1.GET("/:namespace/pods/:name/logs/stream", h.StreamPodLogs)
		// Exec (WebSocket) and file copy
		v1.GET("/:namespace/pods/:name/exec", h.ExecPod)
		v1.GET("/:namespace/pods/:name/cp", h.CopyFromPod)
		v1.POST("/:namespace/pods/:name/cp", h.CopyToPod)
	}

	return r
}
