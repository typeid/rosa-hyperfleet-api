package render

import (
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	seaplanev1alpha1 "github.com/openshift-online/rosa-seaplane-api/api/v1alpha1"
)

const (
	clusterIDLabel    = "seaplane.io/cluster-id"
	resourceTypeLabel = "seaplane.io/resource-type"
	managedByLabel    = "seaplane.io/managed-by"
	managedByOperator = "seaplane-operator"

	// HCPSlotLabel names the hosted control plane slot, zero-padded ("07"), on
	// the slot's subnets, NodePool and nodes, and on the HostedClusters placed
	// on it. HyperShift's ControlPlaneAutoscalingConfiguration on the MC only
	// autoscales HostedClusters with this label.
	HCPSlotLabel = "seaplane.io/hcp-slot"
	// hcpWarmupLabel is set on a HostedCluster while HCPOptions.Warmup is true;
	// the MC's autoscaling configuration raises its control plane's minimum
	// requests while VPA has no usage history yet.
	hcpWarmupLabel = "seaplane.io/hcp-warmup"
	// controlPlaneNodeLabel marks the nodes HyperShift control plane pods run on.
	controlPlaneNodeLabel = "hypershift.openshift.io/control-plane"
)

// HCPWarmupPeriod is how long after the Cluster's creation its HostedCluster
// carries the warm-up label.
const HCPWarmupPeriod = 48 * time.Hour

// InHCPWarmup reports whether a Cluster created at created is still in its
// warm-up period at now.
func InHCPWarmup(created, now time.Time) bool {
	return now.Before(created.Add(HCPWarmupPeriod))
}

// HCPOptions are the per-Cluster settings for running its hosted control
// plane: the slot it runs in (from its Placement) and whether it's still in
// its warm-up period (from the Cluster's age). The cluster controller sets both.
type HCPOptions struct {
	// Slot is the Placement's HCP slot (spec.hcpSlot). Every placed Cluster has
	// one. 0 means the slot is unknown, which only happens when deletion runs
	// again after the Placement is gone; the slot resources are then left out,
	// their desires having been handled on the first pass.
	Slot int32
	// Warmup sets the warm-up label on the HostedCluster (see InHCPWarmup).
	Warmup bool
}

// MaxHCPSlots is the most slots a management cluster can have, so that every
// slot name is two digits.
const MaxHCPSlots = 99

// HCPSlotName is a slot's name in labels and resource names: its number
// zero-padded to two digits, matching Terraform and the hcp-slots chart.
func HCPSlotName(slot int32) string {
	return fmt.Sprintf("%02d", slot)
}

// hostedControlPlaneNamespace is the namespace HyperShift creates for a
// HostedCluster's control plane (hypershift pkg/manifests.HostedControlPlaneNamespace).
func hostedControlPlaneNamespace(hostedClusterNamespace, hostedClusterName string) string {
	return fmt.Sprintf("%s-%s", hostedClusterNamespace, strings.ReplaceAll(hostedClusterName, ".", "-"))
}

// HostedClusterName resolves the internal management-cluster name persisted
// by the platform API or operator. Legacy and automatic-DNS Clusters fall back
// to their existing customer name.
func HostedClusterName(cluster *seaplanev1alpha1.Cluster) string {
	if cluster == nil {
		return ""
	}
	if name := cluster.Spec.HostedClusterName; name != "" && name != seaplanev1alpha1.HostedClusterNamePending {
		return name
	}
	return cluster.Name
}

// RegionalConfig holds per-region values injected by the operator at startup.
type RegionalConfig struct {
	// BaseDomainSuffix is the shared parent zone every cluster's assigned base
	// domain hangs off, e.g. "rosa.example.com". A cluster's own base domain is
	// assembled as "{prefix}.{shard}.{BaseDomainSuffix}".
	BaseDomainSuffix string
	AWSRegion        string
	// ControlPlaneOperatorImage, when non-empty, is stamped onto every
	// HostedCluster as the hypershift.openshift.io/control-plane-operator-image
	// annotation. Development-only override for testing custom
	// control-plane-operator builds; empty disables the override.
	ControlPlaneOperatorImage string
}

// Resource is a generated Kubernetes resource with its GVR for desire creation.
type Resource struct {
	Group     string
	Version   string
	Resource  string
	Name      string
	Namespace string
	Object    any
	// RetainOnDelete marks a resource that HyperShift deletes itself with the
	// HostedCluster. On Cluster deletion its ApplyDesire is removed, so
	// kube-applier stops applying it, instead of being turned into a Delete.
	RetainOnDelete bool
}

// ManagementNamespace returns the namespace used for a Cluster's resources on
// its management cluster. FleetDB namespaces are account-scoped and are not
// used to derive this namespace.
func ManagementNamespace(clusterUID string) string {
	return "cluster-" + clusterUID
}

func clusterLabels(clusterUID string) map[string]string {
	return map[string]string{clusterIDLabel: clusterUID}
}

func clusterResourceLabels(clusterUID, resourceType string) map[string]string {
	labels := clusterLabels(clusterUID)
	labels[resourceTypeLabel] = resourceType
	return labels
}

func managementClusterIdentity(cluster *seaplanev1alpha1.Cluster) (uid, namespace string, err error) {
	if cluster == nil {
		return "", "", fmt.Errorf("cluster is nil")
	}
	if cluster.UID == "" {
		return "", "", fmt.Errorf("cluster %s/%s has no database UID", cluster.Namespace, cluster.Name)
	}
	uid = string(cluster.UID)
	return uid, ManagementNamespace(uid), nil
}

// Minimal local types for CRDs that lack standalone API modules.
// These produce the correct JSON for kube-applier-aws without pulling
// in the full cert-manager or external-secrets dependency trees.

// Certificate is a minimal cert-manager.io/v1.Certificate for serialization.
type Certificate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              CertificateSpec `json:"spec"`
}

type CertificateSpec struct {
	SecretName string               `json:"secretName"`
	IssuerRef  CertificateIssuerRef `json:"issuerRef"`
	DNSNames   []string             `json:"dnsNames,omitempty"`
}

type CertificateIssuerRef struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// ExternalSecret is a minimal external-secrets.io/v1.ExternalSecret for serialization.
type ExternalSecret struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ExternalSecretSpec `json:"spec"`
}

type ExternalSecretSpec struct {
	RefreshInterval string                    `json:"refreshInterval"`
	SecretStoreRef  SecretStoreRef            `json:"secretStoreRef"`
	Target          ExternalSecretTarget      `json:"target"`
	Data            []ExternalSecretDataEntry `json:"data"`
}

type SecretStoreRef struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type ExternalSecretTarget struct {
	Name           string                       `json:"name"`
	CreationPolicy string                       `json:"creationPolicy"`
	Template       ExternalSecretTargetTemplate `json:"template,omitempty"`
}

type ExternalSecretTargetTemplate struct {
	Type string `json:"type"`
}

type ExternalSecretDataEntry struct {
	SecretKey string            `json:"secretKey"`
	RemoteRef ExternalRemoteRef `json:"remoteRef"`
}

type ExternalRemoteRef struct {
	Key string `json:"key"`
}
