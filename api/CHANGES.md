# Changes

## 0.1.1 Oct 15 2026

- chore(api): bump version to api/v0.1.20
- ROSAENG-67909 | feat: identity-flip - client-supplied name + account namespace
- chore(api): bump version to api/v0.1.19
- ROSAENG-68182 | fix: align custom root volumes with V1
- chore(api): bump version to api/v0.1.18
- ROSAENG-65129 | chore: Enable maxUnavailable for v2
- Expose OIDC providers in cluster auth
- ROSAENG-74297: chore: seed CHANGES.md
- chore(api): bump version to api/v0.1.17
- chore(api): bump version to api/v0.1.16
- ROSAENG-65114: feat: add IngressConfiguration Signed-off-by: marcolan018 <llan@redhat.com>
- ROSAENG-65293 | chore: Enable custom AWS tags in v2
- feat(nodepool): expose mutable labels and taints
- fix(validation): validate additional trust bundles
- feat(api): support additional trust bundles
- chore(api): bump version to api/v0.1.15
- fix: preserve cluster proxy configuration
- chore(api): bump version to api/v0.1.14
- ROSAENG-68168 | feat: add schedulerconfiguration into cluster
- chore(api): bump version to api/v0.1.13
- ROSAENG-67988: chore: enable proxyconfiguration passthrough fields
- Add automated versioning and release workflows for api and clientset modules
- ROSAENG-67739 | Expose NodePool fields to SDK/clientset for describe
- Revert "ROSAENG-67489: Remove legacy authz service"
- ROSAENG-67489: Remove legacy authz service
- ROSAENG-66461 | fix: harden pathbind generation
- ROSAENG-66461 | chore: include passthrough depndency in makefile and update docs
- ROSAENG-67515 | fix: update channel write-mode from service-set to mutable
- feat: add ClusterDNS mirror type
- ROSAENG-67262 | feat: scope node pools by cluster
- ROSAENG-67152: fix: remove incorrect APIServer field from ClusterConfiguration
- ROSAENG-67152: feat: add ClusterNetworking mirror type
- ROSAENG-66505 | feat: refactor OIDC to remove DB migrations
- ROSAENG-66479: shard-based DNSReservation decoupled from cluster name
- ROSAENG-66463 | feat: pathbind engine, code generator and draft-as-source pipeline
- ROSAENG-65615 | feat: allow setting pre-created OIDC config on cluster create
- ROSAENG-66043 | feat: refactor managed OIDC flow
- chore!: remove ZOA from platform-api
- chore: batch Renovate minor/patch dependency updates
- ROSAENG-64871 | feat: migrate OidcConfig handler to K8s-native public types
- ROSAENG-64871 | fix: drop /statuses endpoints, improve error mapping, fix nodepool comment
- ROSAENG-64871 | fix: update handler tests and OpenAPI spec for K8s-native types
- ROSAENG-64871 | feat: migrate platform-api to v1alpha1/public types
- review: add clientset markers to generate sdk
- review
- feat: add OidcConfig CRD types and code generation (ROSAENG-65612)
- ROSAENG-65639 | refactor: field registry from flat to per-CRD-type structure
- ROSAENG-65009: close passthrough codegen round-trip pipeline
- remove autoNode, fips, sshKey, osImageStream from required in openapi.yaml
- propagate upstream optional/required markers through passthrough-gen
- ROSAENG-62084 | chore: regenerate all codegen artifacts after bridge marker rename
- ROSAENG-62084 | refactor: rename wire→bridge throughout clientset and api
- ROSAENG-62084 | feat: map cluster_id wire field to NodePool metadata.namespace
- Add ControlPlaneUpgradePolicySpec generated code
- ROSAENG-63001 | feat: add control plane upgrade policies type
- ROSAENG-62596 | fix: set cluster as non namespaced for client generator
- ROSAENG-64737: switch clientset to use kube-like public types
- ROSAENG-61801: fix PR review findings — JSON merge, Configuration merge, buildFieldPath, test compilation
- ROSAENG-61801: make NodePoolSpecPassthrough.Replicas mutable and visible
- ROSAENG-61801: add CRD validation bounds, remove duplicate FIPS, error on unmapped hidden fields
- ROSAENG-61801: wire up conversion-gen pipeline with REST types in api/v1alpha1/public
- ROSAENG-61801: make release field mutable with openapi-gen enabled
- ROSAENG-61803,61805: port service-set preservation, OpenAPI alignment, and codegen enhancements
- ROSAENG-61801: restructure passthrough types into api/v1alpha1
- ROSAENG-64580: move api types from hyperfleet-operator/api to top-level api/


## 0.1.20 Oct 15 2026

- ROSAENG-67909 | feat: identity-flip - client-supplied name + account namespace


## 0.1.19 Oct 13 2026

- ROSAENG-68182 | fix: align custom root volumes with V1
- ROSAENG-65129 | chore: Enable maxUnavailable for v2


## 0.1.18 Oct 12 2026

- Expose OIDC providers in cluster auth
- ROSAENG-74297: chore: seed CHANGES.md


## 0.1.17 Oct 08 2026

- Allow node pool node labels and taints to be updated through the API.
