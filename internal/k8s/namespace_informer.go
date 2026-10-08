package k8s

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
)

// NamespaceStore maintains an in-memory set of namespaces that the running
// identity has successfully accessed. The set is persisted to a ConfigMap
// under the "accessible" key so it survives pod restarts.
//
// Population:
//   - On startup: seeded from the ConfigMap's existing "accessible" key.
//   - At runtime: via AddNamespace, called after a successful resource access.
//
// A SharedInformer watches the ConfigMap so that external updates (e.g. a
// Helm upgrade pre-seeding the list) are merged into the in-memory set.
type NamespaceStore struct {
	mu  sync.RWMutex
	set map[string]struct{}

	c           *Client
	cmNamespace string
	cmName      string
}

func NewNamespaceStore(c *Client, cmNamespace, cmName string) *NamespaceStore {
	return &NamespaceStore{
		c:           c,
		cmNamespace: cmNamespace,
		cmName:      cmName,
		set:         make(map[string]struct{}),
	}
}

// Namespaces returns a sorted copy of the current accessible namespace list.
func (s *NamespaceStore) Namespaces() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sortedKeys()
}

// AddNamespace adds ns to the store. If ns was not already present the updated
// list is persisted to the ConfigMap. Safe to call concurrently.
func (s *NamespaceStore) AddNamespace(ctx context.Context, ns string) {
	s.mu.Lock()
	if _, exists := s.set[ns]; exists {
		s.mu.Unlock()
		return
	}
	s.set[ns] = struct{}{}
	accessible := s.sortedKeys()
	s.mu.Unlock()

	s.persistAccessible(ctx, accessible)
}

// Start seeds the store from the ConfigMap, then launches the informer and
// blocks until ctx is cancelled. Call it in a dedicated goroutine.
func (s *NamespaceStore) Start(ctx context.Context) {
	s.loadFromCM(ctx)

	factory := informers.NewSharedInformerFactoryWithOptions(
		s.c.typed,
		0, // no periodic resync — changes arrive via Watch events only
		informers.WithNamespace(s.cmNamespace),
		informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			opts.FieldSelector = fields.OneTermEqualSelector("metadata.name", s.cmName).String()
		}),
	)

	inf := factory.Core().V1().ConfigMaps().Informer()
	inf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { s.mergeFromObj(obj) },
		UpdateFunc: func(_, obj interface{}) { s.mergeFromObj(obj) },
	})

	factory.Start(ctx.Done())
	cache.WaitForCacheSync(ctx.Done(), inf.HasSynced)
	<-ctx.Done()
}

// loadFromCM performs a direct Get on startup to seed the in-memory set
// before the informer Watch is established.
func (s *NamespaceStore) loadFromCM(ctx context.Context) {
	cm, err := s.c.typed.CoreV1().ConfigMaps(s.cmNamespace).Get(ctx, s.cmName, metav1.GetOptions{})
	if err != nil {
		log.Printf("namespace-store: initial load %s/%s: %v", s.cmNamespace, s.cmName, err)
		return
	}
	s.mergeLines(cm.Data["accessible"])
}

// mergeFromObj is the informer event handler. It merges the "accessible" key
// of the received ConfigMap into the in-memory set (additive: namespaces
// recorded through successful access are never removed at runtime).
func (s *NamespaceStore) mergeFromObj(obj interface{}) {
	cm, ok := obj.(*corev1.ConfigMap)
	if !ok {
		return
	}
	s.mergeLines(cm.Data["accessible"])
}

func (s *NamespaceStore) mergeLines(raw string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ns := range parseLines(raw) {
		s.set[ns] = struct{}{}
	}
}

// persistAccessible patches only the "accessible" key of the ConfigMap.
// If the ConfigMap does not yet exist it is created.
func (s *NamespaceStore) persistAccessible(ctx context.Context, accessible []string) {
	value := strings.Join(accessible, "\n")
	patch := fmt.Sprintf(`{"data":{"accessible":%q}}`, value)
	_, err := s.c.typed.CoreV1().ConfigMaps(s.cmNamespace).Patch(
		ctx, s.cmName, types.MergePatchType, []byte(patch), metav1.PatchOptions{},
	)
	if err == nil {
		return
	}
	if !k8serrors.IsNotFound(err) {
		log.Printf("namespace-store: patch %s/%s: %v", s.cmNamespace, s.cmName, err)
		return
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: s.cmName, Namespace: s.cmNamespace},
		Data:       map[string]string{"accessible": value},
	}
	if _, err = s.c.typed.CoreV1().ConfigMaps(s.cmNamespace).Create(ctx, cm, metav1.CreateOptions{}); err != nil {
		log.Printf("namespace-store: create %s/%s: %v", s.cmNamespace, s.cmName, err)
	}
}

// sortedKeys returns a sorted slice of the set keys. Caller must hold mu.
func (s *NamespaceStore) sortedKeys() []string {
	out := make([]string, 0, len(s.set))
	for ns := range s.set {
		out = append(out, ns)
	}
	sort.Strings(out)
	return out
}

// parseLines splits a newline- or comma-separated list of namespace names.
func parseLines(raw string) []string {
	var out []string
	for _, tok := range strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == ',' }) {
		if ns := strings.TrimSpace(tok); ns != "" {
			out = append(out, ns)
		}
	}
	return out
}
