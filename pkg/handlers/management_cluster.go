package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/openshift/rosa-regional-platform-api/pkg/clients/fleetdb"
	"github.com/openshift/rosa-regional-platform-api/pkg/middleware"
)

// ManagementClusterSpec is the spec stored in the resources table for ManagementCluster.
type ManagementClusterSpec struct {
	Region    string `json:"region"`
	AccountID string `json:"accountId"`
}

// ManagementCluster represents a registered management cluster.
type ManagementCluster struct {
	ID        string `json:"id"`
	Region    string `json:"region"`
	AccountID string `json:"accountId"`
}

// ManagementClusterCreateRequest is the request body for creating an MC registration.
type ManagementClusterCreateRequest struct {
	ID        string `json:"id"`
	Region    string `json:"region"`
	AccountID string `json:"accountId"`
}

// ManagementClusterHandler handles management cluster endpoints.
// It reads and writes ManagementCluster resources in FleetStore (Postgres).
type ManagementClusterHandler struct {
	fleetDB *fleetdb.Client
	logger  *slog.Logger
}

// NewManagementClusterHandler creates a new ManagementClusterHandler.
func NewManagementClusterHandler(fleetDB *fleetdb.Client, logger *slog.Logger) *ManagementClusterHandler {
	return &ManagementClusterHandler{
		fleetDB: fleetDB,
		logger:  logger,
	}
}

// Create handles POST /api/v0/management_clusters
func (h *ManagementClusterHandler) Create(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	accountID := middleware.GetAccountID(ctx)

	h.logger.Info("creating management cluster", "account_id", accountID)

	var req ManagementClusterCreateRequest
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.writeError(w, http.StatusBadRequest, "invalid-request", "Invalid request body")
			return
		}
	}

	if req.ID == "" {
		h.writeError(w, http.StatusBadRequest, "missing-id", "id is required")
		return
	}

	spec := ManagementClusterSpec{
		Region:    req.Region,
		AccountID: req.AccountID,
	}
	specJSON, err := json.Marshal(spec)
	if err != nil {
		h.logger.Error("failed to marshal management cluster spec", "error", err)
		h.writeError(w, http.StatusInternalServerError, "marshal-error", "Failed to process management cluster data")
		return
	}

	_, err = h.fleetDB.DB().Exec(ctx,
		`INSERT INTO resources (kind, namespace, name, spec)
		 VALUES ('ManagementCluster', '_', $1, $2)`,
		req.ID, specJSON,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			h.writeError(w, http.StatusConflict, "already-exists", "Management cluster already registered: "+req.ID)
			return
		}
		h.logger.Error("failed to create management cluster", "error", err)
		h.writeError(w, http.StatusInternalServerError, "config-error", "Failed to save management cluster config")
		return
	}

	mc := ManagementCluster{
		ID:        req.ID,
		Region:    req.Region,
		AccountID: req.AccountID,
	}

	h.logger.Info("management cluster created", "id", mc.ID, "account_id", accountID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(mc)
}

// List handles GET /api/v0/management_clusters
func (h *ManagementClusterHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	accountID := middleware.GetAccountID(ctx)

	h.logger.Debug("listing management clusters", "account_id", accountID)

	rows, err := h.fleetDB.DB().Query(ctx,
		`SELECT name, spec FROM resources
		 WHERE kind = 'ManagementCluster' AND deleted_at IS NULL`,
	)
	if err != nil {
		h.logger.Error("failed to list management clusters", "error", err)
		h.writeError(w, http.StatusInternalServerError, "config-error", "Failed to load management cluster config")
		return
	}
	defer rows.Close()

	var clusters []ManagementCluster
	for rows.Next() {
		var name string
		var specJSON []byte
		if err := rows.Scan(&name, &specJSON); err != nil {
			h.logger.Error("failed to scan management cluster row", "error", err)
			h.writeError(w, http.StatusInternalServerError, "config-error", "Failed to load management cluster config")
			return
		}
		var spec ManagementClusterSpec
		if err := json.Unmarshal(specJSON, &spec); err != nil {
			h.logger.Error("failed to unmarshal management cluster spec", "name", name, "error", err)
			continue
		}
		clusters = append(clusters, ManagementCluster{
			ID:        name,
			Region:    spec.Region,
			AccountID: spec.AccountID,
		})
	}
	if err := rows.Err(); err != nil {
		h.logger.Error("failed to iterate management clusters", "error", err)
		h.writeError(w, http.StatusInternalServerError, "config-error", "Failed to load management cluster config")
		return
	}

	h.logger.Debug("management clusters listed", "total", len(clusters), "account_id", accountID)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"kind":  "ManagementClusterList",
		"items": clusters,
		"total": len(clusters),
	})
}

// Get handles GET /api/v0/management_clusters/{id}
func (h *ManagementClusterHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	accountID := middleware.GetAccountID(ctx)
	vars := mux.Vars(r)
	id := vars["id"]

	h.logger.Debug("getting management cluster", "id", id, "account_id", accountID)

	var specJSON []byte
	err := h.fleetDB.DB().QueryRow(ctx,
		`SELECT spec FROM resources
		 WHERE kind = 'ManagementCluster' AND namespace = '_' AND name = $1 AND deleted_at IS NULL`,
		id,
	).Scan(&specJSON)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			h.writeError(w, http.StatusNotFound, "not-found", "Management cluster not found")
			return
		}
		h.logger.Error("failed to get management cluster", "error", err, "id", id)
		h.writeError(w, http.StatusInternalServerError, "config-error", "Failed to load management cluster config")
		return
	}

	var spec ManagementClusterSpec
	if err := json.Unmarshal(specJSON, &spec); err != nil {
		h.logger.Error("failed to unmarshal management cluster spec", "error", err, "id", id)
		h.writeError(w, http.StatusInternalServerError, "config-error", "Failed to parse management cluster data")
		return
	}

	mc := ManagementCluster{
		ID:        id,
		Region:    spec.Region,
		AccountID: spec.AccountID,
	}

	h.logger.Debug("management cluster retrieved", "id", mc.ID, "account_id", accountID)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(mc)
}

func (h *ManagementClusterHandler) writeError(w http.ResponseWriter, status int, code, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	resp := map[string]interface{}{
		"kind":   "Error",
		"code":   code,
		"reason": reason,
	}

	_ = json.NewEncoder(w).Encode(resp)
}
