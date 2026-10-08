package k8s

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	authv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	sigsyaml "sigs.k8s.io/yaml"
)

// Client wraps the dynamic and typed Kubernetes clients.
type Client struct {
	dynamic    dynamic.Interface
	typed      kubernetes.Interface
	restConfig *rest.Config
	host       string
	cache      gvrCache
	nsStore    *NamespaceStore
}

// ── GVR cache ──────────────────────────────────────────────────────────────

const cacheTTL = 5 * time.Minute

type gvrCache struct {
	mu      sync.RWMutex
	entries map[string]cachedGVR
}

type cachedGVR struct {
	gvr        schema.GroupVersionResource
	namespaced bool
	expiry     time.Time
}

func (c *gvrCache) get(key string) (schema.GroupVersionResource, bool, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[key]
	if !ok || time.Now().After(e.expiry) {
		return schema.GroupVersionResource{}, false, false
	}
	return e.gvr, e.namespaced, true
}

func (c *gvrCache) set(key string, gvr schema.GroupVersionResource, namespaced bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]cachedGVR)
	}
	c.entries[key] = cachedGVR{gvr: gvr, namespaced: namespaced, expiry: time.Now().Add(cacheTTL)}
}

// NewClient builds a Client from a kubeconfig path or falls back to in-cluster → ~/.kube/config.
func NewClient(kubeconfig string) (*Client, error) {
	var cfg *rest.Config
	var err error

	switch {
	case kubeconfig != "":
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	default:
		cfg, err = rest.InClusterConfig()
		if err != nil {
			loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
			cfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
				loadingRules, &clientcmd.ConfigOverrides{},
			).ClientConfig()
		}
	}
	if err != nil {
		return nil, fmt.Errorf("build kubeconfig: %w", err)
	}

	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("dynamic client: %w", err)
	}
	typed, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("typed client: %w", err)
	}

	return &Client{dynamic: dyn, typed: typed, restConfig: cfg, host: cfg.Host}, nil
}

// Host returns the Kubernetes API server address.
func (c *Client) Host() string { return c.host }

// SetNamespaceStore attaches a NamespaceStore that will be used by
// ListNamespaces instead of the cluster-level namespace API.
func (c *Client) SetNamespaceStore(s *NamespaceStore) { c.nsStore = s }

// ── resourceIface: single GVR-resolution point ────────────────────────────

// resourceIface returns the dynamic.ResourceInterface for resourceType in the
// given namespace. It first checks the static GVRByName map, then falls back
// to server-side discovery with a 5-minute TTL cache.
func (c *Client) resourceIface(ctx context.Context, namespace, resourceType string) (dynamic.ResourceInterface, error) {
	if info, ok := GVRByName[resourceType]; ok {
		if info.Namespaced && namespace != "" {
			return c.dynamic.Resource(info.GVR).Namespace(namespace), nil
		}
		return c.dynamic.Resource(info.GVR), nil
	}
	// Dynamic resolution for custom resources
	gvr, namespaced, err := c.resolveGVR(ctx, resourceType)
	if err != nil {
		return nil, fmt.Errorf("unknown resource %q: %w", resourceType, err)
	}
	if namespaced && namespace != "" {
		return c.dynamic.Resource(gvr).Namespace(namespace), nil
	}
	return c.dynamic.Resource(gvr), nil
}

// resolveGVR uses the server-side discovery API to map a plural resource name
// to its GroupVersionResource. Results are cached for cacheTTL.
func (c *Client) resolveGVR(ctx context.Context, resourceType string) (schema.GroupVersionResource, bool, error) {
	if gvr, ns, ok := c.cache.get(resourceType); ok {
		return gvr, ns, nil
	}
	lists, _ := c.typed.Discovery().ServerPreferredResources()
	for _, list := range lists {
		gv, err := schema.ParseGroupVersion(list.GroupVersion)
		if err != nil {
			continue
		}
		for _, r := range list.APIResources {
			if r.Name == resourceType {
				gvr := gv.WithResource(r.Name)
				c.cache.set(resourceType, gvr, r.Namespaced)
				return gvr, r.Namespaced, nil
			}
		}
	}
	return schema.GroupVersionResource{}, false, fmt.Errorf("not found via discovery")
}

// ── CRUD ───────────────────────────────────────────────────────────────────

// ListNamespaces returns all accessible namespace names.
// When a NamespaceStore is attached (namespaced mode), it returns the
// probed-accessible list instead of querying the cluster-level namespace API.
func (c *Client) ListNamespaces(ctx context.Context) ([]string, error) {
	if c.nsStore != nil {
		if ns := c.nsStore.Namespaces(); len(ns) > 0 {
			return ns, nil
		}
		return nil, fmt.Errorf("no accessible namespaces probed yet")
	}
	list, err := c.typed.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	names := make([]string, len(list.Items))
	for i, ns := range list.Items {
		names[i] = ns.Name
	}
	return names, nil
}

// ListResources lists resources of the given type in the namespace.
// Supports both static built-in types and custom resources discovered dynamically.
func (c *Client) ListResources(ctx context.Context, namespace, resourceType string) ([]map[string]interface{}, error) {
	ri, err := c.resourceIface(ctx, namespace, resourceType)
	if err != nil {
		return nil, err
	}
	list, err := ri.List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	items := make([]map[string]interface{}, len(list.Items))
	for i, item := range list.Items {
		items[i] = stripManagedFields(item.Object)
	}
	return items, nil
}

// GetResourceYAML fetches a single resource and returns its YAML representation.
func (c *Client) GetResourceYAML(ctx context.Context, namespace, resourceType, name string) (string, error) {
	ri, err := c.resourceIface(ctx, namespace, resourceType)
	if err != nil {
		return "", err
	}
	obj, err := ri.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	clean := stripManagedFields(obj.Object)
	jsonBytes, err := json.Marshal(clean)
	if err != nil {
		return "", err
	}
	yamlBytes, err := sigsyaml.JSONToYAML(jsonBytes)
	if err != nil {
		return "", err
	}
	return string(yamlBytes), nil
}

// ApplyResourceYAML applies a YAML string to the cluster (server-side apply).
func (c *Client) ApplyResourceYAML(ctx context.Context, namespace, resourceType, name, yamlStr string) error {
	jsonBytes, err := sigsyaml.YAMLToJSON([]byte(yamlStr))
	if err != nil {
		return fmt.Errorf("yaml parse: %w", err)
	}
	ri, err := c.resourceIface(ctx, namespace, resourceType)
	if err != nil {
		return err
	}
	_, err = ri.Patch(ctx, name, types.ApplyPatchType, jsonBytes, metav1.PatchOptions{
		FieldManager: "gubexplorer",
		Force:        boolPtr(true),
	})
	return err
}

// DeleteResource deletes a named resource.
func (c *Client) DeleteResource(ctx context.Context, namespace, resourceType, name string) error {
	ri, err := c.resourceIface(ctx, namespace, resourceType)
	if err != nil {
		return err
	}
	return ri.Delete(ctx, name, metav1.DeleteOptions{})
}

// ScaleResource patches spec.replicas for scalable resources.
// Only resources declared Scalable in the static map are accepted.
func (c *Client) ScaleResource(ctx context.Context, namespace, resourceType, name string, replicas int32) error {
	info, ok := GVRByName[resourceType]
	if !ok || !info.Scalable {
		return fmt.Errorf("resource type %s is not scalable", resourceType)
	}
	patch := []byte(fmt.Sprintf(`{"spec":{"replicas":%d}}`, replicas))
	_, err := c.dynamic.Resource(info.GVR).Namespace(namespace).Patch(
		ctx, name, types.MergePatchType, patch, metav1.PatchOptions{},
	)
	return err
}

// RolloutRestart triggers a rollout restart by patching the pod template annotation.
func (c *Client) RolloutRestart(ctx context.Context, namespace, resourceType, name string) error {
	ri, err := c.resourceIface(ctx, namespace, resourceType)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	patch := []byte(fmt.Sprintf(
		`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":"%s"}}}}}`,
		now,
	))
	_, err = ri.Patch(ctx, name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	return err
}

// GetPodLogs fetches the last tailLines log lines from a pod container.
func (c *Client) GetPodLogs(ctx context.Context, namespace, podName, container string, tailLines int64) (string, error) {
	opts := &corev1.PodLogOptions{
		Container: container,
		TailLines: &tailLines,
	}
	req := c.typed.CoreV1().Pods(namespace).GetLogs(podName, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return "", err
	}
	defer stream.Close()
	data, err := io.ReadAll(stream)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// StreamPodLogs streams pod logs line-by-line, calling onLine for each line.
// It returns when the context is cancelled or the stream ends.
func (c *Client) StreamPodLogs(ctx context.Context, namespace, podName, container string, onLine func(string) error) error {
	tailLines := int64(100)
	follow := true
	opts := &corev1.PodLogOptions{
		Container: container,
		Follow:    follow,
		TailLines: &tailLines,
	}
	req := c.typed.CoreV1().Pods(namespace).GetLogs(podName, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return err
	}
	defer stream.Close()

	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)
	for scanner.Scan() {
		if err := onLine(scanner.Text()); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// GetPodContainers returns the container names of a pod.
func (c *Client) GetPodContainers(ctx context.Context, namespace, podName string) ([]string, error) {
	pod, err := c.typed.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(pod.Spec.Containers)+len(pod.Spec.InitContainers))
	for _, c := range pod.Spec.InitContainers {
		names = append(names, c.Name)
	}
	for _, c := range pod.Spec.Containers {
		names = append(names, c.Name)
	}
	return names, nil
}

// ── Exec & CP ─────────────────────────────────────────────────────────────

// ExecStream opens a remote command execution session on a pod container.
// resizeQueue may be nil for non-TTY sessions.
func (c *Client) ExecStream(
	ctx context.Context,
	namespace, podName, container string,
	command []string,
	stdin io.Reader,
	stdout, stderr io.Writer,
	tty bool,
	resizeQueue remotecommand.TerminalSizeQueue,
) error {
	req := c.typed.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   command,
			Stdin:     stdin != nil,
			Stdout:    stdout != nil,
			Stderr:    stderr != nil,
			TTY:       tty,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(c.restConfig, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("exec: %w", err)
	}
	return executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:             stdin,
		Stdout:            stdout,
		Stderr:            stderr,
		Tty:               tty,
		TerminalSizeQueue: resizeQueue,
	})
}

// CopyFromContainer streams a single file from a container to w via exec cat.
func (c *Client) CopyFromContainer(ctx context.Context, namespace, podName, container, srcPath string, w io.Writer) error {
	var errBuf bytes.Buffer
	err := c.ExecStream(ctx, namespace, podName, container,
		[]string{"cat", srcPath},
		nil, w, &errBuf, false, nil)
	if err != nil {
		return fmt.Errorf("%w: %s", err, errBuf.String())
	}
	return nil
}

// CopyToContainer writes r to dstPath inside a container via exec.
// Uses positional parameter in sh to avoid shell injection.
func (c *Client) CopyToContainer(ctx context.Context, namespace, podName, container, dstPath string, r io.Reader) error {
	var errBuf bytes.Buffer
	err := c.ExecStream(ctx, namespace, podName, container,
		[]string{"sh", "-c", `cat > "$1"`, "--", dstPath},
		r, io.Discard, &errBuf, false, nil)
	if err != nil {
		return fmt.Errorf("%w: %s", err, errBuf.String())
	}
	return nil
}

// stripManagedFields removes metadata.managedFields which is noisy in YAML output.
func stripManagedFields(obj map[string]interface{}) map[string]interface{} {
	if meta, ok := obj["metadata"].(map[string]interface{}); ok {
		delete(meta, "managedFields")
	}
	return obj
}

func boolPtr(b bool) *bool { return &b }

// ── CRD discovery ─────────────────────────────────────────────────────────

// CRDGroup groups custom resource definitions by API group.
type CRDGroup struct {
	Group     string        `json:"group"`
	Resources []CRDResource `json:"resources"`
}

// CRDResource describes a single custom resource type.
type CRDResource struct {
	Name       string `json:"name"` // plural name
	Kind       string `json:"kind"`
	Version    string `json:"version"` // storage version
	Namespaced bool   `json:"namespaced"`
}

// ListCRDTypes returns all custom resource types grouped by API group.
// It first tries the CRD listing API; if that is denied it falls back to
// the server-side discovery API, filtering out known built-in groups.
func (c *Client) ListCRDTypes(ctx context.Context) ([]CRDGroup, string, error) {
	groups, err := c.listCRDsFromCRDAPI(ctx)
	if err == nil {
		return groups, "crds", nil
	}
	groups, err = c.listCRDsFromDiscovery(ctx)
	if err != nil {
		return nil, "", err
	}
	return groups, "discovery", nil
}

// listCRDsFromCRDAPI lists CRD objects via apiextensions.k8s.io/v1.
func (c *Client) listCRDsFromCRDAPI(ctx context.Context) ([]CRDGroup, error) {
	crdGVR := schema.GroupVersionResource{
		Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions",
	}
	list, err := c.dynamic.Resource(crdGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	byGroup := make(map[string]*CRDGroup)
	for _, item := range list.Items {
		group, _, _ := unstructured.NestedString(item.Object, "spec", "group")
		plural, _, _ := unstructured.NestedString(item.Object, "spec", "names", "plural")
		kind, _, _ := unstructured.NestedString(item.Object, "spec", "names", "kind")
		scope, _, _ := unstructured.NestedString(item.Object, "spec", "scope")
		if group == "" || plural == "" {
			continue
		}
		// Find the storage version
		version := ""
		versions, _, _ := unstructured.NestedSlice(item.Object, "spec", "versions")
		for _, v := range versions {
			vm, ok := v.(map[string]interface{})
			if !ok {
				continue
			}
			if stored, _, _ := unstructured.NestedBool(vm, "storage"); stored {
				version, _, _ = unstructured.NestedString(vm, "name")
				break
			}
		}
		if g := byGroup[group]; g == nil {
			byGroup[group] = &CRDGroup{Group: group}
		}
		byGroup[group].Resources = append(byGroup[group].Resources, CRDResource{
			Name: plural, Kind: kind, Version: version, Namespaced: scope == "Namespaced",
		})
	}
	return sortedCRDGroups(byGroup), nil
}

// listCRDsFromDiscovery enumerates API resources from the discovery API and
// returns those whose group is not a known built-in Kubernetes group.
func (c *Client) listCRDsFromDiscovery(ctx context.Context) ([]CRDGroup, error) {
	lists, err := c.typed.Discovery().ServerPreferredResources()
	if err != nil && lists == nil {
		return nil, fmt.Errorf("discovery: %w", err)
	}
	byGroup := make(map[string]*CRDGroup)
	for _, list := range lists {
		gv, parseErr := schema.ParseGroupVersion(list.GroupVersion)
		if parseErr != nil {
			continue
		}
		if knownBuiltinGroup(gv.Group) {
			continue
		}
		for _, r := range list.APIResources {
			if strings.Contains(r.Name, "/") { // skip subresources
				continue
			}
			if _, static := GVRByName[r.Name]; static {
				continue
			}
			if byGroup[gv.Group] == nil {
				byGroup[gv.Group] = &CRDGroup{Group: gv.Group}
			}
			byGroup[gv.Group].Resources = append(byGroup[gv.Group].Resources, CRDResource{
				Name: r.Name, Kind: r.Kind, Version: gv.Version, Namespaced: r.Namespaced,
			})
		}
	}
	return sortedCRDGroups(byGroup), nil
}

// knownBuiltinGroup returns true for API groups that ship with Kubernetes core.
func knownBuiltinGroup(g string) bool {
	switch g {
	case "", "apps", "batch", "autoscaling",
		"networking.k8s.io", "rbac.authorization.k8s.io",
		"apiextensions.k8s.io", "admissionregistration.k8s.io",
		"coordination.k8s.io", "discovery.k8s.io", "events.k8s.io",
		"flowcontrol.apiserver.k8s.io", "node.k8s.io", "policy",
		"scheduling.k8s.io", "storage.k8s.io", "certificates.k8s.io",
		"authorization.k8s.io", "authentication.k8s.io", "metrics.k8s.io":
		return true
	}
	return false
}

func sortedCRDGroups(m map[string]*CRDGroup) []CRDGroup {
	result := make([]CRDGroup, 0, len(m))
	for _, g := range m {
		sort.Slice(g.Resources, func(i, j int) bool { return g.Resources[i].Name < g.Resources[j].Name })
		result = append(result, *g)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Group < result[j].Group })
	return result
}

// ── Permissions ──────────────────────────────────────────────────────────────

// ResourcePermissions holds the RBAC verbs allowed for one resource type.
type ResourcePermissions struct {
	CanList   bool `json:"canList"`
	CanGet    bool `json:"canGet"`
	CanUpdate bool `json:"canUpdate"`
	CanDelete bool `json:"canDelete"`
}

// CheckPermissions queries SelfSubjectRulesReview for the given namespace and
// returns per-resource-type permissions. On error it returns nil (caller treats
// nil as "unknown – assume allowed").
func (c *Client) CheckPermissions(ctx context.Context, namespace string) (map[string]ResourcePermissions, bool, error) {
	review, err := c.typed.AuthorizationV1().SelfSubjectRulesReviews().Create(
		ctx,
		&authv1.SelfSubjectRulesReview{
			Spec: authv1.SelfSubjectRulesReviewSpec{Namespace: namespace},
		},
		metav1.CreateOptions{},
	)
	if err != nil {
		return nil, false, fmt.Errorf("SelfSubjectRulesReview: %w", err)
	}

	result := make(map[string]ResourcePermissions, len(GVRByName))
	for resName, resInfo := range GVRByName {
		var p ResourcePermissions
		for _, rule := range review.Status.ResourceRules {
			if ruleMatches(rule, resInfo) {
				for _, v := range rule.Verbs {
					switch v {
					case "*":
						p.CanList, p.CanGet, p.CanUpdate, p.CanDelete = true, true, true, true
					case "list", "watch":
						p.CanList = true
					case "get":
						p.CanGet = true
					case "update", "patch":
						p.CanUpdate = true
					case "delete", "deletecollection":
						p.CanDelete = true
					}
				}
			}
		}
		result[resName] = p
	}
	return result, review.Status.Incomplete, nil
}

// ruleMatches returns true when the ResourceRule covers the given resource type.
func ruleMatches(rule authv1.ResourceRule, info ResourceInfo) bool {
	groupOK := false
	for _, g := range rule.APIGroups {
		if g == "*" || g == info.GVR.Group {
			groupOK = true
			break
		}
	}
	if !groupOK {
		return false
	}
	for _, r := range rule.Resources {
		if r == "*" || r == info.GVR.Resource {
			return true
		}
	}
	return false
}
