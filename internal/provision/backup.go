// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type BackupAPI interface {
	ListBackups(ctx context.Context, projectID string) ([]map[string]any, error)
	GetBackup(ctx context.Context, backupID string) (map[string]any, error)
	DeleteBackup(ctx context.Context, backupID string) error
	CreateBackup(ctx context.Context, req CreateBackupReq) (map[string]any, error)
	RestoreBackup(ctx context.Context, backupID string, cfg RestoreDiskConfig) (map[string]any, error)
}

// RestoreDiskConfig is the target_config of the restore of a disk backup
// (DiskRestoreConfig): the new disk's name, its type by id or by name, and
// a description; the platform takes the zone and the size from the backup.
type RestoreDiskConfig struct {
	Name         string `json:"name"`
	DiskTypeID   string `json:"disk_type_id,omitempty"`
	DiskTypeName string `json:"disk_type_name,omitempty"`
	Description  string `json:"description,omitempty"`
}

type CreateBackupReq struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	ProjectID   string         `json:"project_id"`
	Resource    BackupResource `json:"resource"`
}

// BackupResource is the source of a backup: a disk or a virtual_machine.
type BackupResource struct {
	ResourceID   string `json:"resource_id"`
	ResourceType string `json:"resource_type"`
}

var _ BackupAPI = (*CloudRuAdapter)(nil)

// ListBackups lists the backups of a project, every page (listRaw); a
// filter of the list (Narrowed) narrows the request.
func (a *CloudRuAdapter) ListBackups(ctx context.Context, projectID string) ([]map[string]any, error) {
	return listRaw(a, ctx, "/api/v1/backups", projectID)
}

// GetBackup reads one backup by id; a backup that is not there is a 404
// error (NOT_FOUND).
func (a *CloudRuAdapter) GetBackup(ctx context.Context, backupID string) (map[string]any, error) {
	resp, err := a.doWithRetry(ctx, http.MethodGet, fmt.Sprintf("/api/v1/backups/%s", seg(backupID)), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

// CreateBackup asks for a backup of a VM or a disk; the answer is the backup,
// with its id and its state (creating).
func (a *CloudRuAdapter) CreateBackup(ctx context.Context, req CreateBackupReq) (map[string]any, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	resp, err := a.doWithRetry(ctx, http.MethodPost, "/api/v1/backups", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

// DeleteBackup asks to delete a backup; the platform answers 204 and deletes
// it after (state deleting, then 404). A DELETE is not repeated after a
// dropped connection (computeRetry).
func (a *CloudRuAdapter) DeleteBackup(ctx context.Context, backupID string) error {
	resp, err := a.doWithRetry(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/backups/%s", seg(backupID)), nil)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

func (a *CloudRuAdapter) RestoreBackup(ctx context.Context, backupID string, cfg RestoreDiskConfig) (map[string]any, error) {
	body, err := json.Marshal(map[string]any{"action": "restore", "resource": map[string]any{"target_config": cfg}})
	if err != nil {
		return nil, err
	}
	resp, err := a.doWithRetry(ctx, http.MethodPost, fmt.Sprintf("/api/v1/backups/%s/action", seg(backupID)), body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}
