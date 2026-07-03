package fleetdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"

	hyperfleetv1alpha1 "github.com/typeid/hyperfleet-operator/api/v1alpha1"
)

// Client wraps a pgxpool.Pool connected to the FleetStore Postgres database.
type Client struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

// NewClient creates a FleetStore client by connecting to Postgres via DSN.
func NewClient(ctx context.Context, dsn string, logger *slog.Logger) (*Client, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect to fleetstore: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping fleetstore: %w", err)
	}
	return &Client{pool: pool, logger: logger}, nil
}

// NewClientFrom wraps an existing pgxpool.Pool (useful for testing).
func NewClientFrom(pool *pgxpool.Pool, logger *slog.Logger) *Client {
	return &Client{pool: pool, logger: logger}
}

// Close closes the underlying connection pool.
func (c *Client) Close() {
	c.pool.Close()
}

// DB returns the underlying connection pool for direct queries.
func (c *Client) DB() *pgxpool.Pool {
	return c.pool
}

// resourceColumns is the standard column list for single-resource queries
// where namespace and name are already known.
const resourceColumns = `uid::text, generation, labels, annotations, owner_refs, finalizers, spec, status, created_at, deletion_timestamp, seq, aws_account_id, updated_at`

// resourceColumnsWithKey includes namespace and name for list queries.
const resourceColumnsWithKey = `namespace, name, ` + resourceColumns

// resourceRow holds a scanned database row from the resources table.
type resourceRow struct {
	Namespace         string
	Name              string
	UID               string
	Generation        int64
	Labels            []byte
	Annotations       []byte
	OwnerRefs         []byte
	Finalizers        []string
	Spec              []byte
	Status            []byte
	CreatedAt         time.Time
	DeletionTimestamp *time.Time
	Seq               int64
	AWSAccountID      *string
	UpdatedAt         time.Time
}

// scanFields returns pointers for scanning the standard resource columns.
func (r *resourceRow) scanFields() []interface{} {
	return []interface{}{
		&r.UID, &r.Generation, &r.Labels, &r.Annotations, &r.OwnerRefs,
		&r.Finalizers, &r.Spec, &r.Status, &r.CreatedAt, &r.DeletionTimestamp,
		&r.Seq, &r.AWSAccountID, &r.UpdatedAt,
	}
}

// scanFieldsWithKey returns pointers for scanning namespace + name + standard columns.
func (r *resourceRow) scanFieldsWithKey() []interface{} {
	return append([]interface{}{&r.Namespace, &r.Name}, r.scanFields()...)
}

func (r *resourceRow) toObjectMeta() metav1.ObjectMeta {
	meta := metav1.ObjectMeta{
		Name:             r.Name,
		Namespace:        r.Namespace,
		UID:              k8stypes.UID(r.UID),
		Generation:       r.Generation,
		ResourceVersion:  strconv.FormatInt(r.Seq, 10),
		CreationTimestamp: metav1.NewTime(r.CreatedAt),
	}
	if r.DeletionTimestamp != nil {
		t := metav1.NewTime(*r.DeletionTimestamp)
		meta.DeletionTimestamp = &t
	}
	if len(r.Labels) > 0 && string(r.Labels) != "null" {
		_ = json.Unmarshal(r.Labels, &meta.Labels)
	}
	if len(r.Annotations) > 0 && string(r.Annotations) != "null" {
		_ = json.Unmarshal(r.Annotations, &meta.Annotations)
	}
	if len(r.OwnerRefs) > 0 && string(r.OwnerRefs) != "null" {
		_ = json.Unmarshal(r.OwnerRefs, &meta.OwnerReferences)
	}
	if len(r.Finalizers) > 0 {
		meta.Finalizers = r.Finalizers
	}
	return meta
}

func (r *resourceRow) toCluster() (*hyperfleetv1alpha1.Cluster, error) {
	cluster := &hyperfleetv1alpha1.Cluster{
		TypeMeta:   clusterTypeMeta,
		ObjectMeta: r.toObjectMeta(),
	}
	if len(r.Spec) > 0 {
		if err := json.Unmarshal(r.Spec, &cluster.Spec); err != nil {
			return nil, fmt.Errorf("unmarshal cluster spec: %w", err)
		}
	}
	if len(r.Status) > 0 {
		if err := json.Unmarshal(r.Status, &cluster.Status); err != nil {
			return nil, fmt.Errorf("unmarshal cluster status: %w", err)
		}
	}
	return cluster, nil
}

func (r *resourceRow) toNodePool() (*hyperfleetv1alpha1.NodePool, error) {
	np := &hyperfleetv1alpha1.NodePool{
		TypeMeta:   nodePoolTypeMeta,
		ObjectMeta: r.toObjectMeta(),
	}
	if len(r.Spec) > 0 {
		if err := json.Unmarshal(r.Spec, &np.Spec); err != nil {
			return nil, fmt.Errorf("unmarshal nodepool spec: %w", err)
		}
	}
	if len(r.Status) > 0 {
		if err := json.Unmarshal(r.Status, &np.Status); err != nil {
			return nil, fmt.Errorf("unmarshal nodepool status: %w", err)
		}
	}
	return np, nil
}

func (r *resourceRow) toManifest() (*hyperfleetv1alpha1.Manifest, error) {
	m := &hyperfleetv1alpha1.Manifest{
		TypeMeta:   manifestTypeMeta,
		ObjectMeta: r.toObjectMeta(),
	}
	if len(r.Spec) > 0 {
		if err := json.Unmarshal(r.Spec, &m.Spec); err != nil {
			return nil, fmt.Errorf("unmarshal manifest spec: %w", err)
		}
	}
	if len(r.Status) > 0 {
		if err := json.Unmarshal(r.Status, &m.Status); err != nil {
			return nil, fmt.Errorf("unmarshal manifest status: %w", err)
		}
	}
	return m, nil
}

var (
	clusterTypeMeta = metav1.TypeMeta{
		Kind:       "Cluster",
		APIVersion: hyperfleetv1alpha1.GroupVersion.String(),
	}
	nodePoolTypeMeta = metav1.TypeMeta{
		Kind:       "NodePool",
		APIVersion: hyperfleetv1alpha1.GroupVersion.String(),
	}
	manifestTypeMeta = metav1.TypeMeta{
		Kind:       "Manifest",
		APIVersion: hyperfleetv1alpha1.GroupVersion.String(),
	}

	clusterGR = schema.GroupResource{
		Group:    hyperfleetv1alpha1.GroupVersion.Group,
		Resource: "clusters",
	}
	nodePoolGR = schema.GroupResource{
		Group:    hyperfleetv1alpha1.GroupVersion.Group,
		Resource: "nodepools",
	}
	manifestGR = schema.GroupResource{
		Group:    hyperfleetv1alpha1.GroupVersion.Group,
		Resource: "manifests",
	}
)

// --- Cluster operations ---

// CreateCluster creates a Cluster row in FleetStore. The cluster is self-namespaced:
// namespace = clusterID, and aws_account_id = accountID.
func (c *Client) CreateCluster(ctx context.Context, accountID string, cluster *hyperfleetv1alpha1.Cluster) error {
	cluster.Namespace = cluster.Name // self-namespaced

	specJSON, err := json.Marshal(cluster.Spec)
	if err != nil {
		return fmt.Errorf("marshal cluster spec: %w", err)
	}
	labelsJSON, err := marshalJSONB(cluster.Labels)
	if err != nil {
		return fmt.Errorf("marshal labels: %w", err)
	}
	annotationsJSON, err := marshalJSONB(cluster.Annotations)
	if err != nil {
		return fmt.Errorf("marshal annotations: %w", err)
	}

	var uid string
	var generation, seq int64
	var createdAt time.Time

	err = c.pool.QueryRow(ctx,
		`INSERT INTO resources (kind, namespace, name, spec, aws_account_id, labels, annotations)
		 VALUES ('Cluster', $1, $2, $3, $4, $5, $6)
		 RETURNING uid::text, generation, seq, created_at`,
		cluster.Name, cluster.Name, specJSON, accountID, labelsJSON, annotationsJSON,
	).Scan(&uid, &generation, &seq, &createdAt)
	if err != nil {
		if isUniqueViolation(err) {
			return apierrors.NewAlreadyExists(clusterGR, cluster.Name)
		}
		return fmt.Errorf("create cluster %s: %w", cluster.Name, err)
	}

	cluster.UID = k8stypes.UID(uid)
	cluster.Generation = generation
	cluster.ResourceVersion = strconv.FormatInt(seq, 10)
	cluster.CreationTimestamp = metav1.NewTime(createdAt)

	return nil
}

// GetCluster retrieves a Cluster by cluster ID.
func (c *Client) GetCluster(ctx context.Context, accountID, clusterID string) (*hyperfleetv1alpha1.Cluster, error) {
	var r resourceRow
	r.Namespace = clusterID
	r.Name = clusterID

	err := c.pool.QueryRow(ctx,
		`SELECT `+resourceColumns+` FROM resources
		 WHERE kind = 'Cluster' AND namespace = $1 AND name = $1 AND deleted_at IS NULL`,
		clusterID,
	).Scan(r.scanFields()...)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierrors.NewNotFound(clusterGR, clusterID)
		}
		return nil, fmt.Errorf("get cluster %s: %w", clusterID, err)
	}

	return r.toCluster()
}

// ListClusters lists Clusters for the given account.
func (c *Client) ListClusters(ctx context.Context, accountID string) (*hyperfleetv1alpha1.ClusterList, error) {
	rows, err := c.pool.Query(ctx,
		`SELECT `+resourceColumnsWithKey+` FROM resources
		 WHERE kind = 'Cluster' AND aws_account_id = $1 AND deleted_at IS NULL`,
		accountID,
	)
	if err != nil {
		return nil, fmt.Errorf("list clusters in %s: %w", accountID, err)
	}
	defer rows.Close()

	var list hyperfleetv1alpha1.ClusterList
	for rows.Next() {
		var r resourceRow
		if err := rows.Scan(r.scanFieldsWithKey()...); err != nil {
			return nil, fmt.Errorf("scan cluster row: %w", err)
		}
		cluster, err := r.toCluster()
		if err != nil {
			return nil, err
		}
		list.Items = append(list.Items, *cluster)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate clusters in %s: %w", accountID, err)
	}

	return &list, nil
}

// UpdateCluster updates the spec of an existing Cluster using CAS on seq.
func (c *Client) UpdateCluster(ctx context.Context, cluster *hyperfleetv1alpha1.Cluster) error {
	specJSON, err := json.Marshal(cluster.Spec)
	if err != nil {
		return fmt.Errorf("marshal cluster spec: %w", err)
	}
	labelsJSON, err := marshalJSONB(cluster.Labels)
	if err != nil {
		return fmt.Errorf("marshal labels: %w", err)
	}
	annotationsJSON, err := marshalJSONB(cluster.Annotations)
	if err != nil {
		return fmt.Errorf("marshal annotations: %w", err)
	}

	prevSeq, err := strconv.ParseInt(cluster.ResourceVersion, 10, 64)
	if err != nil {
		return fmt.Errorf("parse resource version: %w", err)
	}

	var newSeq, newGen int64
	err = c.pool.QueryRow(ctx,
		`UPDATE resources
		 SET spec = $1, labels = $2, annotations = $3, generation = generation + 1
		 WHERE kind = 'Cluster' AND namespace = $4 AND name = $5 AND seq = $6 AND deleted_at IS NULL
		 RETURNING seq, generation`,
		specJSON, labelsJSON, annotationsJSON,
		cluster.Namespace, cluster.Name, prevSeq,
	).Scan(&newSeq, &newGen)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apierrors.NewConflict(clusterGR, cluster.Name,
				fmt.Errorf("resource version %s has been modified", cluster.ResourceVersion))
		}
		return fmt.Errorf("update cluster %s/%s: %w", cluster.Namespace, cluster.Name, err)
	}

	cluster.ResourceVersion = strconv.FormatInt(newSeq, 10)
	cluster.Generation = newGen

	return nil
}

// DeleteCluster soft-deletes a Cluster.
func (c *Client) DeleteCluster(ctx context.Context, accountID, clusterID string) error {
	tag, err := c.pool.Exec(ctx,
		`UPDATE resources
		 SET deletion_timestamp = now(), deleted_at = now()
		 WHERE kind = 'Cluster' AND namespace = $1 AND name = $1 AND deleted_at IS NULL`,
		clusterID,
	)
	if err != nil {
		return fmt.Errorf("delete cluster %s: %w", clusterID, err)
	}
	if tag.RowsAffected() == 0 {
		return apierrors.NewNotFound(clusterGR, clusterID)
	}
	return nil
}

// --- NodePool operations ---

// CreateNodePool creates a NodePool row in FleetStore.
func (c *Client) CreateNodePool(ctx context.Context, accountID string, np *hyperfleetv1alpha1.NodePool) error {
	np.Namespace = accountID

	specJSON, err := json.Marshal(np.Spec)
	if err != nil {
		return fmt.Errorf("marshal nodepool spec: %w", err)
	}
	labelsJSON, err := marshalJSONB(np.Labels)
	if err != nil {
		return fmt.Errorf("marshal labels: %w", err)
	}
	annotationsJSON, err := marshalJSONB(np.Annotations)
	if err != nil {
		return fmt.Errorf("marshal annotations: %w", err)
	}

	var uid string
	var generation, seq int64
	var createdAt time.Time

	err = c.pool.QueryRow(ctx,
		`INSERT INTO resources (kind, namespace, name, spec, aws_account_id, labels, annotations)
		 VALUES ('NodePool', $1, $2, $3, $4, $5, $6)
		 RETURNING uid::text, generation, seq, created_at`,
		accountID, np.Name, specJSON, accountID, labelsJSON, annotationsJSON,
	).Scan(&uid, &generation, &seq, &createdAt)
	if err != nil {
		if isUniqueViolation(err) {
			return apierrors.NewAlreadyExists(nodePoolGR, np.Name)
		}
		return fmt.Errorf("create nodepool %s/%s: %w", accountID, np.Name, err)
	}

	np.UID = k8stypes.UID(uid)
	np.Generation = generation
	np.ResourceVersion = strconv.FormatInt(seq, 10)
	np.CreationTimestamp = metav1.NewTime(createdAt)

	return nil
}

// GetNodePool retrieves a NodePool by account ID and nodepool ID.
func (c *Client) GetNodePool(ctx context.Context, accountID, nodepoolID string) (*hyperfleetv1alpha1.NodePool, error) {
	var r resourceRow
	r.Namespace = accountID
	r.Name = nodepoolID

	err := c.pool.QueryRow(ctx,
		`SELECT `+resourceColumns+` FROM resources
		 WHERE kind = 'NodePool' AND namespace = $1 AND name = $2 AND deleted_at IS NULL`,
		accountID, nodepoolID,
	).Scan(r.scanFields()...)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierrors.NewNotFound(nodePoolGR, nodepoolID)
		}
		return nil, fmt.Errorf("get nodepool %s/%s: %w", accountID, nodepoolID, err)
	}

	return r.toNodePool()
}

// ListNodePools lists NodePools for the given account, optionally filtered by clusterRef.
func (c *Client) ListNodePools(ctx context.Context, accountID, clusterRef string) (*hyperfleetv1alpha1.NodePoolList, error) {
	var rows pgx.Rows
	var err error

	if clusterRef != "" {
		rows, err = c.pool.Query(ctx,
			`SELECT `+resourceColumnsWithKey+` FROM resources
			 WHERE kind = 'NodePool' AND aws_account_id = $1 AND deleted_at IS NULL
			 AND spec->>'clusterRef' = $2`,
			accountID, clusterRef,
		)
	} else {
		rows, err = c.pool.Query(ctx,
			`SELECT `+resourceColumnsWithKey+` FROM resources
			 WHERE kind = 'NodePool' AND aws_account_id = $1 AND deleted_at IS NULL`,
			accountID,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("list nodepools in %s: %w", accountID, err)
	}
	defer rows.Close()

	var list hyperfleetv1alpha1.NodePoolList
	for rows.Next() {
		var r resourceRow
		if err := rows.Scan(r.scanFieldsWithKey()...); err != nil {
			return nil, fmt.Errorf("scan nodepool row: %w", err)
		}
		np, err := r.toNodePool()
		if err != nil {
			return nil, err
		}
		list.Items = append(list.Items, *np)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate nodepools in %s: %w", accountID, err)
	}

	return &list, nil
}

// UpdateNodePool updates the spec of an existing NodePool using CAS on seq.
func (c *Client) UpdateNodePool(ctx context.Context, np *hyperfleetv1alpha1.NodePool) error {
	specJSON, err := json.Marshal(np.Spec)
	if err != nil {
		return fmt.Errorf("marshal nodepool spec: %w", err)
	}
	labelsJSON, err := marshalJSONB(np.Labels)
	if err != nil {
		return fmt.Errorf("marshal labels: %w", err)
	}
	annotationsJSON, err := marshalJSONB(np.Annotations)
	if err != nil {
		return fmt.Errorf("marshal annotations: %w", err)
	}

	prevSeq, err := strconv.ParseInt(np.ResourceVersion, 10, 64)
	if err != nil {
		return fmt.Errorf("parse resource version: %w", err)
	}

	var newSeq, newGen int64
	err = c.pool.QueryRow(ctx,
		`UPDATE resources
		 SET spec = $1, labels = $2, annotations = $3, generation = generation + 1
		 WHERE kind = 'NodePool' AND namespace = $4 AND name = $5 AND seq = $6 AND deleted_at IS NULL
		 RETURNING seq, generation`,
		specJSON, labelsJSON, annotationsJSON,
		np.Namespace, np.Name, prevSeq,
	).Scan(&newSeq, &newGen)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apierrors.NewConflict(nodePoolGR, np.Name,
				fmt.Errorf("resource version %s has been modified", np.ResourceVersion))
		}
		return fmt.Errorf("update nodepool %s/%s: %w", np.Namespace, np.Name, err)
	}

	np.ResourceVersion = strconv.FormatInt(newSeq, 10)
	np.Generation = newGen

	return nil
}

// DeleteNodePool soft-deletes a NodePool.
func (c *Client) DeleteNodePool(ctx context.Context, accountID, nodepoolID string) error {
	tag, err := c.pool.Exec(ctx,
		`UPDATE resources
		 SET deletion_timestamp = now(), deleted_at = now()
		 WHERE kind = 'NodePool' AND namespace = $1 AND name = $2 AND deleted_at IS NULL`,
		accountID, nodepoolID,
	)
	if err != nil {
		return fmt.Errorf("delete nodepool %s/%s: %w", accountID, nodepoolID, err)
	}
	if tag.RowsAffected() == 0 {
		return apierrors.NewNotFound(nodePoolGR, nodepoolID)
	}
	return nil
}

// --- Manifest operations ---

// CreateManifest creates a Manifest row in FleetStore.
func (c *Client) CreateManifest(ctx context.Context, namespace string, hfm *hyperfleetv1alpha1.Manifest) error {
	hfm.Namespace = namespace

	specJSON, err := json.Marshal(hfm.Spec)
	if err != nil {
		return fmt.Errorf("marshal manifest spec: %w", err)
	}
	labelsJSON, err := marshalJSONB(hfm.Labels)
	if err != nil {
		return fmt.Errorf("marshal labels: %w", err)
	}
	annotationsJSON, err := marshalJSONB(hfm.Annotations)
	if err != nil {
		return fmt.Errorf("marshal annotations: %w", err)
	}

	var uid string
	var generation, seq int64
	var createdAt time.Time

	err = c.pool.QueryRow(ctx,
		`INSERT INTO resources (kind, namespace, name, spec, labels, annotations)
		 VALUES ('Manifest', $1, $2, $3, $4, $5)
		 RETURNING uid::text, generation, seq, created_at`,
		namespace, hfm.Name, specJSON, labelsJSON, annotationsJSON,
	).Scan(&uid, &generation, &seq, &createdAt)
	if err != nil {
		if isUniqueViolation(err) {
			return apierrors.NewAlreadyExists(manifestGR, hfm.Name)
		}
		return fmt.Errorf("create manifest %s/%s: %w", namespace, hfm.Name, err)
	}

	hfm.UID = k8stypes.UID(uid)
	hfm.Generation = generation
	hfm.ResourceVersion = strconv.FormatInt(seq, 10)
	hfm.CreationTimestamp = metav1.NewTime(createdAt)

	return nil
}

// GetManifest retrieves a Manifest by namespace and name.
func (c *Client) GetManifest(ctx context.Context, namespace, name string) (*hyperfleetv1alpha1.Manifest, error) {
	var r resourceRow
	r.Namespace = namespace
	r.Name = name

	err := c.pool.QueryRow(ctx,
		`SELECT `+resourceColumns+` FROM resources
		 WHERE kind = 'Manifest' AND namespace = $1 AND name = $2 AND deleted_at IS NULL`,
		namespace, name,
	).Scan(r.scanFields()...)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierrors.NewNotFound(manifestGR, name)
		}
		return nil, fmt.Errorf("get manifest %s/%s: %w", namespace, name, err)
	}

	return r.toManifest()
}

// DeleteManifest soft-deletes a Manifest.
func (c *Client) DeleteManifest(ctx context.Context, namespace, name string) error {
	tag, err := c.pool.Exec(ctx,
		`UPDATE resources
		 SET deletion_timestamp = now(), deleted_at = now()
		 WHERE kind = 'Manifest' AND namespace = $1 AND name = $2 AND deleted_at IS NULL`,
		namespace, name,
	)
	if err != nil {
		return fmt.Errorf("delete manifest %s/%s: %w", namespace, name, err)
	}
	if tag.RowsAffected() == 0 {
		return apierrors.NewNotFound(manifestGR, name)
	}
	return nil
}

// --- Error helpers ---

// IsNotFound returns true if the error is a Kubernetes 404.
func IsNotFound(err error) bool {
	return apierrors.IsNotFound(err)
}

// IsAlreadyExists returns true if the error is a Kubernetes 409 (already exists).
func IsAlreadyExists(err error) bool {
	return apierrors.IsAlreadyExists(err)
}

// --- internal helpers ---

// isUniqueViolation checks if a Postgres error is a unique constraint violation (23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// marshalJSONB marshals a value to JSON, returning nil for nil/empty maps.
func marshalJSONB(v interface{}) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}
