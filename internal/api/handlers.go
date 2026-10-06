package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"gubexplorer/internal/k8s"

	"github.com/gin-gonic/gin"
)

// Handler holds shared dependencies for all HTTP handlers.
type Handler struct {
	client    *k8s.Client
	defaultNS string
	version   string
}

// ---------- Permissions ----------

// GetPermissions returns per-resource-type RBAC permissions for a namespace
// using SelfSubjectRulesReview. The frontend uses the result to disable
// inaccessible actions and flag locked resources in the sidebar.
func (h *Handler) GetPermissions(c *gin.Context) {
	namespace := c.Param("namespace")

	perms, incomplete, err := h.client.CheckPermissions(c.Request.Context(), namespace)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"permissions": map[string]interface{}{},
			"incomplete":  true,
			"error":       err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"permissions": perms,
		"incomplete":  incomplete,
	})
}

// ---------- CRD types ----------

// ListCRDTypes returns all custom resource type groups for the sidebar.
func (h *Handler) ListCRDTypes(c *gin.Context) {
	groups, source, err := h.client.ListCRDTypes(c.Request.Context())
	if err != nil {
		c.JSON(statusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"groups": groups, "source": source})
}

// ---------- Config ----------

// GetConfig returns app-level configuration to the frontend.
func (h *Handler) GetConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"defaultNamespace": h.defaultNS,
		"version":          h.version,
		"host":             h.client.Host(),
	})
}

// ---------- Namespaces ----------

// ListNamespaces returns all accessible namespaces.
// On permission error it falls back to returning only the default namespace.
func (h *Handler) ListNamespaces(c *gin.Context) {
	namespaces, err := h.client.ListNamespaces(c.Request.Context())
	if err != nil {
		// Fallback: return just the default namespace when RBAC denies cluster-level access.
		c.JSON(http.StatusOK, gin.H{"namespaces": []string{h.defaultNS}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"namespaces": namespaces})
}

// ---------- Resource types ----------

// ListResourceTypes returns the sidebar resource group tree.
func (h *Handler) ListResourceTypes(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"groups": k8s.Groups})
}

// ---------- Generic resource CRUD ----------

// ListResources lists all resources of a given type in a namespace.
func (h *Handler) ListResources(c *gin.Context) {
	namespace := c.Param("namespace")
	resource := c.Param("resource")

	items, err := h.client.ListResources(c.Request.Context(), namespace, resource)
	if err != nil {
		c.JSON(statusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

// GetResource returns the YAML of a single resource.
func (h *Handler) GetResource(c *gin.Context) {
	namespace := c.Param("namespace")
	resource := c.Param("resource")
	name := c.Param("name")

	yaml, err := h.client.GetResourceYAML(c.Request.Context(), namespace, resource, name)
	if err != nil {
		c.JSON(statusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"yaml": yaml})
}

// UpdateResource applies a YAML body to an existing resource.
func (h *Handler) UpdateResource(c *gin.Context) {
	namespace := c.Param("namespace")
	resource := c.Param("resource")
	name := c.Param("name")

	var body struct {
		YAML string `json:"yaml"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.client.ApplyResourceYAML(c.Request.Context(), namespace, resource, name, body.YAML); err != nil {
		c.JSON(statusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "updated"})
}

// DeleteResource deletes a named resource.
func (h *Handler) DeleteResource(c *gin.Context) {
	namespace := c.Param("namespace")
	resource := c.Param("resource")
	name := c.Param("name")

	if err := h.client.DeleteResource(c.Request.Context(), namespace, resource, name); err != nil {
		c.JSON(statusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("%s/%s deleted", resource, name)})
}

// ---------- Actions ----------

// ScaleResource changes the replica count for scalable resources.
func (h *Handler) ScaleResource(c *gin.Context) {
	namespace := c.Param("namespace")
	resource := c.Param("resource")
	name := c.Param("name")

	var body struct {
		Replicas int32 `json:"replicas"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.client.ScaleResource(c.Request.Context(), namespace, resource, name, body.Replicas); err != nil {
		c.JSON(statusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("scaled to %d", body.Replicas)})
}

// RestartResource triggers a rollout restart.
func (h *Handler) RestartResource(c *gin.Context) {
	namespace := c.Param("namespace")
	resource := c.Param("resource")
	name := c.Param("name")

	if err := h.client.RolloutRestart(c.Request.Context(), namespace, resource, name); err != nil {
		c.JSON(statusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "restart triggered"})
}

// ---------- Pod-specific ----------

// GetPodContainers returns the container names of a pod.
func (h *Handler) GetPodContainers(c *gin.Context) {
	namespace := c.Param("namespace")
	podName := c.Param("name")

	containers, err := h.client.GetPodContainers(c.Request.Context(), namespace, podName)
	if err != nil {
		c.JSON(statusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"containers": containers})
}

// GetPodLogs returns the last N log lines (default 500) of a pod container.
func (h *Handler) GetPodLogs(c *gin.Context) {
	namespace := c.Param("namespace")
	podName := c.Param("name")
	container := c.Query("container")
	tail := int64(500)
	if t := c.Query("tail"); t != "" {
		if v, err := strconv.ParseInt(t, 10, 64); err == nil {
			tail = v
		}
	}

	logs, err := h.client.GetPodLogs(c.Request.Context(), namespace, podName, container, tail)
	if err != nil {
		c.JSON(statusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"logs": logs})
}

// StreamPodLogs streams pod logs via Server-Sent Events.
func (h *Handler) StreamPodLogs(c *gin.Context) {
	namespace := c.Param("namespace")
	podName := c.Param("name")
	container := c.Query("container")

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	ctx := c.Request.Context()

	err := h.client.StreamPodLogs(ctx, namespace, podName, container, func(line string) error {
		c.SSEvent("log", line)
		c.Writer.Flush()
		return nil
	})
	if err != nil && ctx.Err() == nil {
		c.SSEvent("error", err.Error())
		c.Writer.Flush()
	}
}

// ---------- helpers ----------

// statusCode maps common Kubernetes errors to HTTP status codes.
func statusCode(err error) int {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "not found"):
		return http.StatusNotFound
	case strings.Contains(msg, "forbidden"), strings.Contains(msg, "unauthorized"):
		return http.StatusForbidden
	case strings.Contains(msg, "already exists"):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
