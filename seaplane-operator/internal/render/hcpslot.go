package render

import (
	hypershiftv1beta1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

const (
	// hcpCapacityBufferName names the PodTemplate and CapacityBuffer that keep
	// spare room on a slotted control plane's nodes.
	hcpCapacityBufferName = "hcp-capacity-buffer"
	// hcpCapacityBufferChunks is one chunk per availability zone.
	hcpCapacityBufferChunks = 3
	// pauseImage fills the PodTemplate's required image. Buffer pods are
	// virtual: they exist only in Karpenter's scheduling simulation, so the
	// image is never pulled.
	pauseImage = "registry.k8s.io/pause:3.10"
)

// hcpCapacityBufferChunk is the spare room kept per availability zone. It
// covers the warm-up resize of kube-apiserver, etcd, openshift-apiserver and
// kube-controller-manager on one node, so that resize happens in place.
// Perf-scale tests set the final size.
var hcpCapacityBufferChunk = corev1.ResourceList{
	corev1.ResourceCPU:    resource.MustParse("2"),
	corev1.ResourceMemory: resource.MustParse("8Gi"),
}

// CapacityBuffer is a minimal Karpenter autoscaling.x-k8s.io/v1beta1
// CapacityBuffer for serialization.
type CapacityBuffer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              CapacityBufferSpec `json:"spec"`
}

type CapacityBufferSpec struct {
	// PodTemplateRef names the PodTemplate, in the same namespace, that defines one chunk.
	PodTemplateRef corev1.LocalObjectReference `json:"podTemplateRef"`
	// Replicas is the number of chunks.
	Replicas *int32 `json:"replicas,omitempty"`
}

// hcpNodeSelector selects the nodes of one control plane in its slot: the
// slot's NodePool sets the control plane and slot labels, and Karpenter sets
// the cluster label from what the pending pods select.
func hcpNodeSelector(hcpNamespace, slot string) map[string]string {
	return map[string]string{
		controlPlaneNodeLabel:                "true",
		hypershiftv1beta1.HostedClusterLabel: hcpNamespace,
		HCPSlotLabel:                         slot,
	}
}

// hcpSlotResources are the MC resources a slotted control plane needs besides
// its HostedCluster: its namespace labeled with the slot, which the MC's pod
// placement guardrail checks every control plane pod against, and a capacity
// buffer on its nodes.
func hcpSlotResources(clusterID, hcpNamespace, slot string) []Resource {
	return []Resource{
		controlPlaneNamespace(clusterID, hcpNamespace, slot),
		hcpCapacityBufferPodTemplate(clusterID, hcpNamespace, slot),
		hcpCapacityBuffer(clusterID, hcpNamespace),
	}
}

// controlPlaneNamespace pre-creates the namespace HyperShift runs the control
// plane in, labeled with its slot. HyperShift adds its own labels to the
// existing namespace and deletes it with the HostedCluster. Workloads in the
// namespace can't change the label: namespaces are cluster-scoped.
func controlPlaneNamespace(clusterID, hcpNamespace, slot string) Resource {
	labels := clusterResourceLabels(clusterID, "control-plane-namespace")
	labels[managedByLabel] = managedByOperator
	labels[HCPSlotLabel] = slot
	return Resource{
		Group: "", Version: "v1", Resource: "namespaces",
		Name: hcpNamespace, Namespace: "",
		Object: &corev1.Namespace{
			TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"},
			ObjectMeta: metav1.ObjectMeta{
				Name:   hcpNamespace,
				Labels: labels,
			},
		},
		// Deleting it alongside the HostedCluster would stop HyperShift's teardown
		// and cloud resource cleanup midway.
		RetainOnDelete: true,
	}
}

// hcpCapacityBufferPodTemplate is the shape of one buffer chunk: it runs only
// on this control plane's nodes, one chunk per zone.
func hcpCapacityBufferPodTemplate(clusterID, hcpNamespace, slot string) Resource {
	podLabels := map[string]string{"app": hcpCapacityBufferName}
	return Resource{
		Group: "", Version: "v1", Resource: "podtemplates",
		Name: hcpCapacityBufferName, Namespace: hcpNamespace,
		Object: &corev1.PodTemplate{
			TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PodTemplate"},
			ObjectMeta: metav1.ObjectMeta{
				Name:      hcpCapacityBufferName,
				Namespace: hcpNamespace,
				Labels:    clusterResourceLabels(clusterID, "capacity-buffer"),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: podLabels},
				Spec: corev1.PodSpec{
					NodeSelector: hcpNodeSelector(hcpNamespace, slot),
					Tolerations: []corev1.Toleration{{
						Key:      controlPlaneNodeLabel,
						Operator: corev1.TolerationOpEqual,
						Value:    "true",
						Effect:   corev1.TaintEffectNoSchedule,
					}},
					TopologySpreadConstraints: []corev1.TopologySpreadConstraint{{
						MaxSkew:           1,
						TopologyKey:       corev1.LabelTopologyZone,
						WhenUnsatisfiable: corev1.DoNotSchedule,
						LabelSelector:     &metav1.LabelSelector{MatchLabels: podLabels},
					}},
					Containers: []corev1.Container{{
						Name:      "buffer",
						Image:     pauseImage,
						Resources: corev1.ResourceRequirements{Requests: hcpCapacityBufferChunk},
					}},
				},
			},
		},
	}
}

func hcpCapacityBuffer(clusterID, hcpNamespace string) Resource {
	return Resource{
		Group: "autoscaling.x-k8s.io", Version: "v1beta1", Resource: "capacitybuffers",
		Name: hcpCapacityBufferName, Namespace: hcpNamespace,
		Object: &CapacityBuffer{
			TypeMeta: metav1.TypeMeta{APIVersion: "autoscaling.x-k8s.io/v1beta1", Kind: "CapacityBuffer"},
			ObjectMeta: metav1.ObjectMeta{
				Name:      hcpCapacityBufferName,
				Namespace: hcpNamespace,
				Labels:    clusterResourceLabels(clusterID, "capacity-buffer"),
			},
			Spec: CapacityBufferSpec{
				PodTemplateRef: corev1.LocalObjectReference{Name: hcpCapacityBufferName},
				Replicas:       ptr.To[int32](hcpCapacityBufferChunks),
			},
		},
	}
}
