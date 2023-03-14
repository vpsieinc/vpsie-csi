package govpsie

import (
	"context"
	"fmt"
	"net/http"
)

var storageBasePath = "/apps/v2"

type StorageService interface {
	List(ctx context.Context, options *ListOptions) ([]Storage, error)
	Delete(ctx context.Context, storageIdentifier string) error
	AttachToVPSie(ctx context.Context, storageIdentifier, vmIdentifier string) error
	DetachToVPSie(ctx context.Context, storageIdentifier, vmIdentifier string) error
	CreateContainer(ctx context.Context, dcIdentifier string) error
	ListAll(ctx context.Context, options *ListOptions) ([]Storage, error)
	Update(ctx context.Context, updateReq *StorageUpdateRequest) error
	Create(ctx context.Context, createReq *StorageCreateRequest, vmIdentifier string) error
}

type storageServiceHandler struct {
	client *Client
}

type Storage struct {
	ID             int    `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	UserID         int    `json:"user_id"`
	BoxID          int    `json:"box_id"`
	Identifier     string `json:"identifier"`
	UserTemplateID int    `json:"user_template_id"`
	StorageType    string `json:"storage_type"`
	DiskFormat     string `json:"disk_format"`
	IsAutomatic    int    `json:"is_automatic"`
	Size           int    `json:"size"`
	StorageID      int    `json:"storage_id"`
	DiskKey        string `json:"disk_key"`
	CreatedOn      string `json:"created_on"`
	VmIdentifier   string `json:"vmIdentifier"`
	Hostname       string `json:"hostname"`
	OsIdentifier   string `json:"osIdentifier"`
	State          string `json:"state"`
	DcIdentifier   string `json:"dcIdentifier"`
}

type ListStorageRoot struct {
	Error bool      `json:"error"`
	Data  []Storage `json:"data"`
	Total int       `json:"total"`
}

type StorageUpdateRequest struct {
	StorageIdentifier string `json:"storageIdentifier"`
	Name              string `json:"name"`
	Description       string `json:"description"`
	Size              int    `json:"size"`
}

type StorageCreateRequest struct {
	Name         string   `json:"name"`
	DcIdentifier string   `json:"dcIdentifier"`
	Description  string   `json:"description"`
	Size         int      `json:"size"`
	StorageType  string   `json:"storageType"`
	DiskFormat   string   `json:"diskFormat"`
	Tags         []string `json:"tags"`
	IsAutomatic  int      `json:"isAutomatic"`
}

func (s *storageServiceHandler) List(ctx context.Context, options *ListOptions) ([]Storage, error) {
	path := fmt.Sprintf("%s/storages?offset=%d&limit%d", storageBasePath, options.Page, options.PerPage)

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	storages := new(ListStorageRoot)
	if err = s.client.Do(ctx, req, &storages); err != nil {
		return nil, err
	}

	return storages.Data, nil
}

func (s *storageServiceHandler) Delete(ctx context.Context, storageIdentifier string) error {
	path := fmt.Sprintf("%s/storages", storageBasePath)

	deleteRequest := struct {
		StorageIdentifier string `json:"storageIdentifier"`
	}{
		StorageIdentifier: storageIdentifier,
	}

	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, &deleteRequest)
	if err != nil {
		return err
	}

	return s.client.Do(ctx, req, nil)
}

func (s *storageServiceHandler) AttachToVPSie(ctx context.Context, storageIdentifier, vmIdentifier string) error {

	path := fmt.Sprintf("%s/vm/attach", storageBasePath)

	attachReq := struct {
		StorageIdentifier string `json:"storageIdentifier"`
		VmIdentifier      string `json:"vmIdentifier"`
	}{
		StorageIdentifier: storageIdentifier,
		VmIdentifier:      vmIdentifier,
	}

	req, err := s.client.NewRequest(ctx, http.MethodPost, path, &attachReq)
	if err != nil {
		return err
	}

	return s.client.Do(ctx, req, nil)

}

func (s *storageServiceHandler) DetachToVPSie(ctx context.Context, storageIdentifier, vmIdentifier string) error {

	path := fmt.Sprintf("%s/vm/detach", storageBasePath)

	detachReq := struct {
		StorageIdentifier string `json:"storageIdentifier"`
		VmIdentifier      string `json:"vmIdentifier"`
	}{
		StorageIdentifier: storageIdentifier,
		VmIdentifier:      vmIdentifier,
	}

	req, err := s.client.NewRequest(ctx, http.MethodPost, path, &detachReq)
	if err != nil {
		return err
	}

	return s.client.Do(ctx, req, nil)

}
func (s *storageServiceHandler) CreateContainer(ctx context.Context, dcIdentifier string) error {

	path := fmt.Sprintf("%s/storages/create/container", storageBasePath)

	createContainerReq := struct {
		DcIdentifier string `json:"dcIdentifier"`
	}{
		DcIdentifier: dcIdentifier,
	}

	req, err := s.client.NewRequest(ctx, http.MethodPost, path, createContainerReq)
	if err != nil {
		return err
	}

	return s.client.Do(ctx, req, nil)
}

func (s *storageServiceHandler) Update(ctx context.Context, updateReq *StorageUpdateRequest) error {
	path := fmt.Sprintf("%s/storages/edit", storageBasePath)

	req, err := s.client.NewRequest(ctx, http.MethodPut, path, &updateReq)
	if err != nil {
		return err
	}

	return s.client.Do(ctx, req, nil)

}

func (s *storageServiceHandler) ListAll(ctx context.Context, options *ListOptions) ([]Storage, error) {
	path := fmt.Sprintf("%s/storages", storageBasePath)

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	storages := new(ListStorageRoot)
	if err = s.client.Do(ctx, req, &storages); err != nil {
		return nil, err
	}
	return storages.Data, nil
}

func (s *storageServiceHandler) Create(ctx context.Context, createReq *StorageCreateRequest, vmIdentifier string) error {
	path := fmt.Sprintf("%s/storages/vm/attach/all", storageBasePath)

	createPayload := struct {
		Storages     []StorageCreateRequest `json:"storages"`
		VmIdentifier string                 `json:"vmIdentifier"`
	}{
		Storages: []StorageCreateRequest{
			*createReq,
		},
		VmIdentifier: vmIdentifier,
	}

	req, err := s.client.NewRequest(ctx, http.MethodPost, path, createPayload)
	if err != nil {
		return err
	}

	return s.client.Do(ctx, req, nil)
}
