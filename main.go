package main

import (
	"flag"
	"log"
	"os"
	"strings"

	"gubexplorer/internal/api"
	"gubexplorer/internal/k8s"
)

// version is injected at build time via -ldflags "-X main.version=x.y.z"
var version = "dev"

func main() {
	port := flag.String("port", envOr("PORT", "8080"), "HTTP listen port")
	kubeconfig := flag.String("kubeconfig", os.Getenv("KUBECONFIG"), "Path to kubeconfig file (empty = in-cluster or ~/.kube/config)")
	namespace := flag.String("namespace", os.Getenv("POD_NAMESPACE"), "Default namespace (empty = auto-detect)")
	authUser := flag.String("auth-user", envOr("AUTH_USERNAME", ""), "Basic auth username (empty = auth disabled)")
	authPass := flag.String("auth-password", envOr("AUTH_PASSWORD", ""), "Basic auth password")
	flag.Parse()

	if *namespace == "" {
		*namespace = detectNamespace()
	}

	client, err := k8s.NewClient(*kubeconfig)
	if err != nil {
		log.Fatalf("failed to create kubernetes client: %v", err)
	}

	if *authUser == "" || *authPass == "" {
		log.Fatal("basic auth is required: set --auth-user / --auth-password or AUTH_USERNAME / AUTH_PASSWORD env vars")
	}

	if *authUser != "" {
		log.Printf("starting gubexplorer on :%s  |  default namespace: %s  |  auth: enabled (user=%s)", *port, *namespace, *authUser)
	}

	router := api.NewRouter(client, *namespace, *authUser, *authPass, version)
	if err := router.Run(":" + *port); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func detectNamespace() string {
	if data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
		return strings.TrimSpace(string(data))
	}
	return "default"
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
