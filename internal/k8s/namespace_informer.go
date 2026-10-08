package k8s

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	authv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
)

// NamespaceStore probes a user-supplied list of candidate namespaces for
// accessibility and maintains an in-memory list of reachable ones.
// It uses a SharedInformer on a ConfigMap to react to changes in the
// candidate list, and persists accessible namespaces back to the same
// ConfigMap under the "accessible" key.
type NamespaceStore struct {
	mu         sync.RWMutex
	namespaces []string

	c           *Client
	cmNamespace string
	cmName      string
}

// NewNamespaceStore creates a NamespaceStore backed by the named ConfigMap in
// cmNamespace.
func NewNamespaceStore(c *Client, cmNamespace, cmName string) *NamespaceStore {
	return &NamespaceStore{c: c, cmNamespace: cmNamespace, cmName: cmName}
}

// Namespaces returns a copy of the current accessible namespace list.
func (s *NamespaceStore) Namespaces() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.namespaces))
	copy(out, s.namespaces)
	return out
}

// Start launches the ConfigMap informer and blocks until ctx is cancelled.
// Call it in a dedicated goroutine.
func (s *NamespaceStore) Start(ctx context.Context) {
	factory := informers.NewSharedInformerFactoryWithOptions(
		s.c.typed,
		5*time.Minute,
		informers.WithNamespace(s.cmNamespace),
		informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			opts.FieldSelector = fields.OneTermEqualSelector("metadata.name", s.cmName).String()
		}),
	)

	inf := factory.Core().V1().ConfigMaps().Informer()
	inf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(_ interface{}) { s.reconcile(ctx) },
		UpdateFunc: func(_, _ interface{}) { s.reconcile(ctx) },
	})

	factory.Start(ctx.Done())
	cache.WaitForCacheSync(ctx.Done(), inf.HasSynced)
	// Initial reconcile in case the Add event was already processed before the
	// handler was registered (race), or the ConfigMap predates this run.
	s.reconcile(ctx)
}

// reconcile reads the candidates key, probes each namespace, updates the
// in-memory list, and persists the result back to the ConfigMap.
func (s *NamespaceStore) reconcile(ctx context.Context) {
	cm, err := s.c.typed.CoreV1().ConfigMaps(s.cmNamespace).Get(ctx, s.cmName, metav1.GetOptions{})
	if err != nil {
		log.Printf("namespace-store: get ConfigMap %s/%s: %v", s.cmNamespace, s.cmName, err)
		return
	}

	candidates := parseCandidates(cm.Data["candidates"])
	if len(candidates) == 0 {
		return
	}

	accessible := s.probeAll(ctx, candidates)

	s.mu.Lock()
	s.namespaces = accessible
	s.mu.Unlock()

	s.persistAccessible(ctx, accessible)
}

// probeAll checks each candidate namespace via SelfSubjectRulesReview and
// returns those where the running identity can list or get at least one known
// namespaced resource.
func (s *NamespaceStore) probeAll(ctx context.Context, candidates []string) []string {
	var out []string
	for _, ns := range candidates {
		if s.isAccessible(ctx, ns) {
			out = append(out, ns)
		}
	}
	sort.Strings(out)
	return out
}

func (s *NamespaceStore) isAccessible(ctx context.Context, ns string) bool {
	review, err := s.c.typed.AuthorizationV1().SelfSubjectRulesReviews().Create(
		ctx,
		&authv1.SelfSubjectRulesReview{
			Spec: authv1.SelfSubjectRulesReviewSpec{Namespace: ns},
		},
		metav1.CreateOptions{},
	)
	if err != nil {
		return false
	}
	for _, info := range GVRByName {
		if !info.Namespaced {
			continue
		}
		for _, rule := range review.Status.ResourceRules {
			if !ruleMatches(rule, info) {
				continue
			}
			for _, v := range rule.Verbs {
				if v == "list" || v == "get" || v == "watch" || v == "*" {
					return true
				}
			}
		}
	}
	return false
}

// persistAccessible patches the "accessible" key of the ConfigMap using a
// merge patch so we never overwrite the user-managed "candidates" key.
func (s *NamespaceStore) persistAccessible(ctx context.Context, accessible []string) {
	patch := fmt.Sprintf(`{"data":{"accessible":%q}}`, strings.Join(accessible, "\n"))
	if _, err := s.c.typed.CoreV1().ConfigMaps(s.cmNamespace).Patch(
		ctx, s.cmName, types.MergePatchType, []byte(patch), metav1.PatchOptions{},
	); err != nil {
		log.Printf("namespace-store: patch ConfigMap %s/%s: %v", s.cmNamespace, s.cmName, err)
	}
}

// parseCandidates splits a newline- or comma-separated list of namespace names.
func parseCandidates(raw string) []string {
	var out []string
	for _, tok := range strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == ',' }) {
		if ns := strings.TrimSpace(tok); ns != "" {
			out = append(out, ns)
		}
	}
	return out
}
