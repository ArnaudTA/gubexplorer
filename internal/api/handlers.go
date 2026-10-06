package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"gubexplorer/internal/k8s"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"k8s.io/client-go/tools/remotecommand"
)

// Handler holds shared dependencies for all HTTP handlers.
type Handler struct {
	client         *k8s.Client
	defaultNS      string
	saNamespace    string // actual namespace the pod runs in (for RBAC hints)
	serviceAccount string // name of the ServiceAccount the pod runs as
	version        string
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
		"saNamespace":      h.saNamespace,
		"serviceAccount":   h.serviceAccount,
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

// ---------- Exec ----------

// wsUpgrader upgrades HTTP connections to WebSocket for exec sessions.
var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// Allow any origin since the app uses Basic Auth for access control.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// wsWriter is a thread-safe io.Writer that sends binary WebSocket messages.
type wsWriter struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

func (w *wsWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.conn.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// termSizeQueue implements remotecommand.TerminalSizeQueue via a channel.
type termSizeQueue struct{ ch chan remotecommand.TerminalSize }

func (q *termSizeQueue) Next() *remotecommand.TerminalSize {
	s, ok := <-q.ch
	if !ok {
		return nil
	}
	return &s
}

// ExecPod upgrades the HTTP connection to a WebSocket and streams a shell
// session inside the requested pod container.
//
// WebSocket framing (client → server):
//   - byte[0] == 0x01 : resize event, rest is JSON {"cols":N,"rows":N}
//   - otherwise       : raw stdin bytes
//
// WebSocket framing (server → client): raw stdout/stderr bytes
func (h *Handler) ExecPod(c *gin.Context) {
	namespace := c.Param("namespace")
	podName := c.Param("name")
	container := c.Query("container")
	shell := c.DefaultQuery("shell", "/bin/sh")

	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	stdinR, stdinW := io.Pipe()
	defer stdinW.Close()

	sizeQ := &termSizeQueue{ch: make(chan remotecommand.TerminalSize, 1)}
	defer close(sizeQ.ch)

	// Read WebSocket messages → stdin or resize events
	go func() {
		defer stdinW.Close()
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				cancel()
				return
			}
			if len(msg) > 0 && msg[0] == 0x01 {
				var rs struct {
					Cols uint16 `json:"cols"`
					Rows uint16 `json:"rows"`
				}
				if json.Unmarshal(msg[1:], &rs) == nil {
					select {
					case sizeQ.ch <- remotecommand.TerminalSize{Width: rs.Cols, Height: rs.Rows}:
					default:
					}
				}
				continue
			}
			if _, err := stdinW.Write(msg); err != nil {
				return
			}
		}
	}()

	stdout := &wsWriter{conn: conn}
	err = h.client.ExecStream(ctx, namespace, podName, container,
		[]string{shell}, stdinR, stdout, stdout, true, sizeQ)
	if err != nil && ctx.Err() == nil {
		stdout.Write([]byte("\r\n\x1b[31m[Session ended: " + err.Error() + "]\x1b[0m\r\n"))
	} else {
		stdout.Write([]byte("\r\n\x1b[33m[Connection closed]\x1b[0m\r\n"))
	}
}

// ---------- CP ----------

// CopyFromPod streams a file from a container as an HTTP download.
// Query params: container, path
func (h *Handler) CopyFromPod(c *gin.Context) {
	namespace := c.Param("namespace")
	podName := c.Param("name")
	container := c.Query("container")
	path := c.Query("path")
	if path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path is required"})
		return
	}

	filename := filepath.Base(path)
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Header("Content-Type", "application/octet-stream")

	if err := h.client.CopyFromContainer(c.Request.Context(), namespace, podName, container, path, c.Writer); err != nil {
		// Headers already sent; write error inline.
		c.Writer.Write([]byte("\nError: " + err.Error())) //nolint:errcheck
	}
}

// CopyToPod uploads the request body to a file path inside a container.
// Query params: container, path
func (h *Handler) CopyToPod(c *gin.Context) {
	namespace := c.Param("namespace")
	podName := c.Param("name")
	container := c.Query("container")
	path := c.Query("path")
	if path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path is required"})
		return
	}

	if err := h.client.CopyToContainer(c.Request.Context(), namespace, podName, container, path, c.Request.Body); err != nil {
		c.JSON(statusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "uploaded to " + path})
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
