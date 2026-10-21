package controller

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	seaplanev1alpha1 "github.com/openshift-online/rosa-seaplane-api/api/v1alpha1"
	"github.com/openshift-online/rosa-seaplane-api/seaplane-operator/internal/render"
)

// A Cluster claims an HCP slot on its management cluster with an Index named
// after the slot ("07") in the management cluster's slot namespace, labeled
// with the Cluster's UID. The database primary key lets only one Cluster claim
// each slot, even when operator replicas place Clusters at the same time.
//
// Claims are read through the manager's client, which on seaplane-db reads
// Postgres directly and across all shards. Never read them from the cache: it
// only holds this replica's shard, so it misses other replicas' claims.

// hcpSlotNamespacePrefix prefixes the Index namespace that holds a management
// cluster's HCP slot claims.
const hcpSlotNamespacePrefix = "hcp-slots-"

// hcpSlotNamespace is the uniqueness domain for one management cluster's HCP slots.
func hcpSlotNamespace(managementCluster string) string {
	return hcpSlotNamespacePrefix + managementCluster
}

type hcpSlotClaim struct {
	index             *seaplanev1alpha1.Index
	managementCluster string
	slot              int32
}

// claimHCPSlot claims the lowest free HCP slot on a management cluster.
// Returns 0 if every slot is taken.
func (r *PlacementReconciler) claimHCPSlot(ctx context.Context, cluster *seaplanev1alpha1.Cluster, mc string) (int32, error) {
	namespace := hcpSlotNamespace(mc)
	var claimed seaplanev1alpha1.IndexList
	if err := r.List(ctx, &claimed, client.InNamespace(namespace)); err != nil {
		return 0, fmt.Errorf("list HCP slot claims on %s: %w", mc, err)
	}
	taken := make(map[string]bool, len(claimed.Items))
	for i := range claimed.Items {
		taken[claimed.Items[i].Name] = true
	}

	for slot := int32(1); slot <= int32(r.HCPSlotCount); slot++ {
		name := render.HCPSlotName(slot)
		if taken[name] {
			continue
		}
		claim := &seaplanev1alpha1.Index{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels:    map[string]string{ownerUIDLabel: string(cluster.UID)},
			},
		}
		if err := r.Create(ctx, claim); err != nil {
			if apierrors.IsAlreadyExists(err) {
				// Another Cluster claimed it since the list; try the next slot.
				continue
			}
			return 0, fmt.Errorf("claim HCP slot %s on %s: %w", name, mc, err)
		}
		return slot, nil
	}
	return 0, nil
}

// releaseHCPSlotClaims deletes the HCP slot claims held by a Cluster UID,
// except the one keep places it on (nil releases every claim).
func releaseHCPSlotClaims(ctx context.Context, c client.Client, clusterUID types.UID, keep *seaplanev1alpha1.PlacementSpec) error {
	claims, err := ownedHCPSlotClaims(ctx, c, clusterUID)
	if err != nil {
		return err
	}
	for _, claim := range claims {
		if keep != nil && claim.managementCluster == keep.ManagementCluster && claim.slot == keep.HCPSlot {
			continue
		}
		if err := c.Delete(ctx, claim.index); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("release HCP slot %s/%s: %w", claim.index.Namespace, claim.index.Name, err)
		}
	}
	return nil
}

// ownedHCPSlotClaims lists the HCP slot claims held by a Cluster UID, ordered
// by management cluster and slot.
func ownedHCPSlotClaims(ctx context.Context, reader client.Reader, clusterUID types.UID) ([]hcpSlotClaim, error) {
	var indexes seaplanev1alpha1.IndexList
	if err := reader.List(ctx, &indexes, client.MatchingLabels{ownerUIDLabel: string(clusterUID)}); err != nil {
		return nil, fmt.Errorf("list HCP slot claims of Cluster UID %s: %w", clusterUID, err)
	}
	var claims []hcpSlotClaim
	for i := range indexes.Items {
		index := &indexes.Items[i]
		mc, ok := strings.CutPrefix(index.Namespace, hcpSlotNamespacePrefix)
		if !ok {
			// Another kind of Index owned by this UID.
			continue
		}
		slot, err := strconv.ParseInt(index.Name, 10, 32)
		if err != nil || slot < 1 {
			continue
		}
		claims = append(claims, hcpSlotClaim{index: index, managementCluster: mc, slot: int32(slot)})
	}
	sort.Slice(claims, func(i, j int) bool {
		if claims[i].managementCluster != claims[j].managementCluster {
			return claims[i].managementCluster < claims[j].managementCluster
		}
		return claims[i].slot < claims[j].slot
	})
	return claims, nil
}
