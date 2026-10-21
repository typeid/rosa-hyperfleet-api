/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	seaplanev1alpha1 "github.com/openshift-online/rosa-seaplane-api/api/v1alpha1"
)

var _ = Describe("Placement Controller HCP slots", func() {
	const testNS = "account-123456789012"

	ctx := context.Background()
	var created []string

	createMC := func(name string) {
		mc := &seaplanev1alpha1.ManagementCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec:       seaplanev1alpha1.ManagementClusterSpec{Region: "us-east-1", AccountID: "123456789012"},
		}
		if err := k8sClient.Create(ctx, mc); err != nil {
			Expect(apierrors.IsAlreadyExists(err)).To(BeTrue(), "create ManagementCluster %s: %v", name, err)
		}
		ensureNamespace(ctx, hcpSlotNamespace(name))
	}

	createCluster := func(name string) *seaplanev1alpha1.Cluster {
		cluster := newTestCluster(name)
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		created = append(created, name)
		return cluster
	}

	reconcileCluster := func(r *PlacementReconciler, name string) error {
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: testNS, Name: name}})
		return err
	}

	getPlacement := func(name string) seaplanev1alpha1.Placement {
		var placement seaplanev1alpha1.Placement
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: testNS, Name: name + ".placement"}, &placement)).To(Succeed())
		return placement
	}

	claimsOf := func(cluster *seaplanev1alpha1.Cluster) []string {
		claims, err := ownedHCPSlotClaims(ctx, k8sClient, cluster.UID)
		Expect(err).NotTo(HaveOccurred())
		var names []string
		for _, claim := range claims {
			names = append(names, claim.index.Namespace+"/"+claim.index.Name)
		}
		return names
	}

	claim := func(cluster *seaplanev1alpha1.Cluster, mc, slot string) {
		Expect(k8sClient.Create(ctx, &seaplanev1alpha1.Index{
			ObjectMeta: metav1.ObjectMeta{
				Name:      slot,
				Namespace: hcpSlotNamespace(mc),
				Labels:    map[string]string{ownerUIDLabel: string(cluster.UID)},
			},
		})).To(Succeed())
	}

	BeforeEach(func() {
		ensureNamespace(ctx, testNS)
		createMC("mc01")
		created = nil
	})

	AfterEach(func() {
		for _, name := range created {
			cluster := &seaplanev1alpha1.Cluster{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: testNS, Name: name}, cluster); err == nil {
				controllerutil.RemoveFinalizer(cluster, clusterFinalizer)
				_ = k8sClient.Update(ctx, cluster)
				_ = k8sClient.Delete(ctx, cluster)
			}
			_ = k8sClient.Delete(ctx, &seaplanev1alpha1.Placement{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNS, Name: name + ".placement"},
			})
		}
		for _, mc := range []string{"mc01", "mc02"} {
			_ = k8sClient.DeleteAllOf(ctx, &seaplanev1alpha1.Index{}, client.InNamespace(hcpSlotNamespace(mc)))
		}
		_ = k8sClient.Delete(ctx, &seaplanev1alpha1.ManagementCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc02"}})
	})

	It("places each Cluster on its own lowest free slot", func() {
		r := &PlacementReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), HCPSlotCount: 3}
		first := createCluster("hcp-slot-first")
		second := createCluster("hcp-slot-second")

		Expect(reconcileCluster(r, first.Name)).To(Succeed())
		Expect(reconcileCluster(r, second.Name)).To(Succeed())

		Expect(getPlacement(first.Name).Spec).To(Equal(seaplanev1alpha1.PlacementSpec{
			ClusterName: first.Name, ManagementCluster: "mc01", HCPSlot: 1,
		}))
		Expect(getPlacement(second.Name).Spec.HCPSlot).To(Equal(int32(2)))
		Expect(claimsOf(first)).To(Equal([]string{"hcp-slots-mc01/01"}))
		Expect(claimsOf(second)).To(Equal([]string{"hcp-slots-mc01/02"}))
	})

	It("doesn't place a Cluster when every slot is taken", func() {
		r := &PlacementReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), HCPSlotCount: 1}
		first := createCluster("hcp-slot-full-first")
		second := createCluster("hcp-slot-full-second")
		Expect(reconcileCluster(r, first.Name)).To(Succeed())

		err := reconcileCluster(r, second.Name)
		Expect(err).To(MatchError(ContainSubstring("no management cluster has a free HCP slot")))

		var placement seaplanev1alpha1.Placement
		err = k8sClient.Get(ctx, types.NamespacedName{Namespace: testNS, Name: second.Name + ".placement"}, &placement)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
		Expect(claimsOf(second)).To(BeEmpty())
	})

	It("places a Cluster on the next management cluster when one is full", func() {
		createMC("mc02")
		r := &PlacementReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), HCPSlotCount: 1}
		first := createCluster("hcp-slot-spill-first")
		second := createCluster("hcp-slot-spill-second")

		Expect(reconcileCluster(r, first.Name)).To(Succeed())
		Expect(reconcileCluster(r, second.Name)).To(Succeed())

		Expect(getPlacement(first.Name).Spec.ManagementCluster).To(Equal("mc01"))
		placement := getPlacement(second.Name)
		Expect(placement.Spec.ManagementCluster).To(Equal("mc02"))
		Expect(placement.Spec.HCPSlot).To(Equal(int32(1)))
	})

	It("reuses a slot claimed by an earlier attempt", func() {
		r := &PlacementReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), HCPSlotCount: 3}
		cluster := createCluster("hcp-slot-recover")
		// A previous reconcile claimed slot 2 and stopped before creating the Placement.
		claim(cluster, "mc01", "02")

		Expect(reconcileCluster(r, cluster.Name)).To(Succeed())

		Expect(getPlacement(cluster.Name).Spec.HCPSlot).To(Equal(int32(2)))
		Expect(claimsOf(cluster)).To(Equal([]string{"hcp-slots-mc01/02"}))
	})

	It("releases its own claim when another attempt wrote the Placement first", func() {
		cluster := createCluster("hcp-slot-stray")
		// Right after this attempt claims slot 1, a racing attempt (another
		// operator replica) writes the Placement with slot 5, which it claimed.
		raced := false
		racer := &createHook{Client: k8sClient, afterCreate: func(obj client.Object) {
			if _, ok := obj.(*seaplanev1alpha1.Index); !ok || raced {
				return
			}
			raced = true
			claim(cluster, "mc01", "05")
			Expect(k8sClient.Create(ctx, &seaplanev1alpha1.Placement{
				ObjectMeta: metav1.ObjectMeta{Name: cluster.Name + ".placement", Namespace: testNS},
				Spec: seaplanev1alpha1.PlacementSpec{
					ClusterName: cluster.Name, ManagementCluster: "mc01", HCPSlot: 5,
				},
			})).To(Succeed())
		}}
		r := &PlacementReconciler{Client: racer, Scheme: k8sClient.Scheme(), HCPSlotCount: 6}

		Expect(reconcileCluster(r, cluster.Name)).To(Succeed())

		Expect(getPlacement(cluster.Name).Spec.HCPSlot).To(Equal(int32(5)))
		Expect(claimsOf(cluster)).To(Equal([]string{"hcp-slots-mc01/05"}))
	})

	It("doesn't look up slot claims for a Cluster that is already placed", func() {
		cluster := createCluster("hcp-slot-steady")
		Expect(reconcileCluster(&PlacementReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), HCPSlotCount: 3}, cluster.Name)).To(Succeed())

		counter := &indexListCounter{Client: k8sClient}
		r := &PlacementReconciler{Client: counter, Scheme: k8sClient.Scheme(), HCPSlotCount: 3}
		Expect(reconcileCluster(r, cluster.Name)).To(Succeed())

		Expect(counter.lists).To(BeZero())
	})

	It("releases the slot when the Cluster's resources are cleaned up", func() {
		r := &PlacementReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), HCPSlotCount: 1}
		first := createCluster("hcp-slot-release-first")
		Expect(reconcileCluster(r, first.Name)).To(Succeed())
		Expect(claimsOf(first)).To(Equal([]string{"hcp-slots-mc01/01"}))

		// Cleanup runs once the HostedCluster is gone from the MC.
		clusters := &ClusterReconciler{Client: k8sClient}
		_, err := clusters.cleanupAndRemoveFinalizer(ctx, first)
		Expect(err).NotTo(HaveOccurred())
		Expect(claimsOf(first)).To(BeEmpty())

		// The freed slot goes to the next Cluster.
		second := createCluster("hcp-slot-release-second")
		Expect(reconcileCluster(r, second.Name)).To(Succeed())
		Expect(getPlacement(second.Name).Spec.HCPSlot).To(Equal(int32(1)))
	})

	It("releases the claimed slot when the Cluster is deleted while placing it", func() {
		cluster := createCluster("hcp-slot-deleted-midway")
		// The Cluster is deleted, and its cleanup finds no claims, right after
		// the slot claim is created.
		deleteOnClaim := &createHook{Client: k8sClient, afterCreate: func(obj client.Object) {
			if _, ok := obj.(*seaplanev1alpha1.Index); !ok {
				return
			}
			var latest seaplanev1alpha1.Cluster
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), &latest)).To(Succeed())
			controllerutil.RemoveFinalizer(&latest, clusterFinalizer)
			Expect(k8sClient.Update(ctx, &latest)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &latest)).To(Succeed())
		}}
		r := &PlacementReconciler{Client: deleteOnClaim, Scheme: k8sClient.Scheme(), HCPSlotCount: 3}

		Expect(reconcileCluster(r, cluster.Name)).To(Succeed())

		Expect(claimsOf(cluster)).To(BeEmpty())
		var placement seaplanev1alpha1.Placement
		err := k8sClient.Get(ctx, types.NamespacedName{Namespace: testNS, Name: cluster.Name + ".placement"}, &placement)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	It("keeps a Placement's slot immutable", func() {
		r := &PlacementReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), HCPSlotCount: 3}
		cluster := createCluster("hcp-slot-immutable")
		Expect(reconcileCluster(r, cluster.Name)).To(Succeed())

		placement := getPlacement(cluster.Name)
		placement.Spec.HCPSlot = 3
		Expect(k8sClient.Update(ctx, &placement)).To(MatchError(ContainSubstring("spec.hcpSlot is immutable")))
	})
})

// createHook runs afterCreate after every successful Create.
type createHook struct {
	client.Client
	afterCreate func(client.Object)
}

func (h *createHook) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if err := h.Client.Create(ctx, obj, opts...); err != nil {
		return err
	}
	h.afterCreate(obj)
	return nil
}

// indexListCounter counts List calls for Indexes.
type indexListCounter struct {
	client.Client
	lists int
}

func (c *indexListCounter) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if _, ok := list.(*seaplanev1alpha1.IndexList); ok {
		c.lists++
	}
	return c.Client.List(ctx, list, opts...)
}
