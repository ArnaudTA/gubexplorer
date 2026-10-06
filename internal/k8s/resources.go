package k8s

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ResourceInfo describes a Kubernetes resource type.
type ResourceInfo struct {
	Name       string // plural resource name used in URLs
	Kind       string
	GVR        schema.GroupVersionResource
	Namespaced bool
	Scalable   bool
}

// ResourceGroup groups related resource types for the UI sidebar.
type ResourceGroup struct {
	Label     string
	Icon      string
	Resources []ResourceInfo
}

// GVRByName maps the URL resource name to its GVR.
var GVRByName = map[string]ResourceInfo{
	"pods": {
		Name: "pods", Kind: "Pod",
		GVR:        schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
		Namespaced: true,
	},
	"deployments": {
		Name: "deployments", Kind: "Deployment",
		GVR:        schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
		Namespaced: true, Scalable: true,
	},
	"statefulsets": {
		Name: "statefulsets", Kind: "StatefulSet",
		GVR:        schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"},
		Namespaced: true, Scalable: true,
	},
	"daemonsets": {
		Name: "daemonsets", Kind: "DaemonSet",
		GVR:        schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"},
		Namespaced: true,
	},
	"replicasets": {
		Name: "replicasets", Kind: "ReplicaSet",
		GVR:        schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"},
		Namespaced: true, Scalable: true,
	},
	"jobs": {
		Name: "jobs", Kind: "Job",
		GVR:        schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"},
		Namespaced: true,
	},
	"cronjobs": {
		Name: "cronjobs", Kind: "CronJob",
		GVR:        schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"},
		Namespaced: true,
	},
	"services": {
		Name: "services", Kind: "Service",
		GVR:        schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"},
		Namespaced: true,
	},
	"ingresses": {
		Name: "ingresses", Kind: "Ingress",
		GVR:        schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"},
		Namespaced: true,
	},
	"networkpolicies": {
		Name: "networkpolicies", Kind: "NetworkPolicy",
		GVR:        schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"},
		Namespaced: true,
	},
	"configmaps": {
		Name: "configmaps", Kind: "ConfigMap",
		GVR:        schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"},
		Namespaced: true,
	},
	"secrets": {
		Name: "secrets", Kind: "Secret",
		GVR:        schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"},
		Namespaced: true,
	},
	"persistentvolumeclaims": {
		Name: "persistentvolumeclaims", Kind: "PersistentVolumeClaim",
		GVR:        schema.GroupVersionResource{Group: "", Version: "v1", Resource: "persistentvolumeclaims"},
		Namespaced: true,
	},
	"serviceaccounts": {
		Name: "serviceaccounts", Kind: "ServiceAccount",
		GVR:        schema.GroupVersionResource{Group: "", Version: "v1", Resource: "serviceaccounts"},
		Namespaced: true,
	},
	"roles": {
		Name: "roles", Kind: "Role",
		GVR:        schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles"},
		Namespaced: true,
	},
	"rolebindings": {
		Name: "rolebindings", Kind: "RoleBinding",
		GVR:        schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "rolebindings"},
		Namespaced: true,
	},
	"horizontalpodautoscalers": {
		Name: "horizontalpodautoscalers", Kind: "HorizontalPodAutoscaler",
		GVR:        schema.GroupVersionResource{Group: "autoscaling", Version: "v2", Resource: "horizontalpodautoscalers"},
		Namespaced: true,
	},
	"events": {
		Name: "events", Kind: "Event",
		GVR:        schema.GroupVersionResource{Group: "", Version: "v1", Resource: "events"},
		Namespaced: true,
	},
	"persistentvolumes": {
		Name: "persistentvolumes", Kind: "PersistentVolume",
		GVR:        schema.GroupVersionResource{Group: "", Version: "v1", Resource: "persistentvolumes"},
		Namespaced: false,
	},
	"nodes": {
		Name: "nodes", Kind: "Node",
		GVR:        schema.GroupVersionResource{Group: "", Version: "v1", Resource: "nodes"},
		Namespaced: false,
	},
}

// Groups defines the sidebar structure returned to the UI.
var Groups = []ResourceGroup{
	{
		Label: "Workloads", Icon: "fa-cubes",
		Resources: []ResourceInfo{
			GVRByName["pods"],
			GVRByName["deployments"],
			GVRByName["statefulsets"],
			GVRByName["daemonsets"],
			GVRByName["replicasets"],
			GVRByName["jobs"],
			GVRByName["cronjobs"],
		},
	},
	{
		Label: "Network", Icon: "fa-network-wired",
		Resources: []ResourceInfo{
			GVRByName["services"],
			GVRByName["ingresses"],
			GVRByName["networkpolicies"],
		},
	},
	{
		Label: "Config & Storage", Icon: "fa-layer-group",
		Resources: []ResourceInfo{
			GVRByName["configmaps"],
			GVRByName["secrets"],
			GVRByName["persistentvolumeclaims"],
			GVRByName["persistentvolumes"],
		},
	},
	{
		Label: "Security", Icon: "fa-shield-halved",
		Resources: []ResourceInfo{
			GVRByName["serviceaccounts"],
			GVRByName["roles"],
			GVRByName["rolebindings"],
		},
	},
	{
		Label: "Autoscaling", Icon: "fa-chart-line",
		Resources: []ResourceInfo{
			GVRByName["horizontalpodautoscalers"],
		},
	},
	{
		Label: "Cluster", Icon: "fa-server",
		Resources: []ResourceInfo{
			GVRByName["nodes"],
			GVRByName["persistentvolumes"],
		},
	},
	{
		Label: "Events", Icon: "fa-bell",
		Resources: []ResourceInfo{
			GVRByName["events"],
		},
	},
}
