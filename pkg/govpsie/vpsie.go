package govpsie

import (
	"context"
	"fmt"
	"net/http"
)

var vpsieBasePath = "/apps/v2/vm"

type VpsieService interface {
	ListVpsie(context.Context, *ListOptions, string) ([]VmData, error)
	GetVpsieByIdentifier(context.Context, string) (*VmData, error)
	GetVpsieStatusByIdentifier(context.Context, string) (*Status, error)
	GetVpsieConsole(ctx context.Context, identifierId string) (*VpsieConsole, error)
	CreateVpsie(context.Context, *CreateVpsieRequest) error
	DeleteVpsie(ctx context.Context, identifierId string) error
	StartVpsie(ctx context.Context, identifierId string) error
	StopVpsie(ctx context.Context, identifierId string) error
	RestartVpsie(ctx context.Context, identifierId string) error
	ChangePassword(ctx context.Context, identifierId string, newPassword string) error
	ChangeHostName(ctx context.Context, identifierId string, newHostname string) error
	AddVPC(ctx context.Context, request *VpcRequest) error
	MoveVPC(ctx context.Context, request *VpcRequest) error
	AddTags(ctx context.Context, identifierId string, tags []string) error
	ResizeVpsie(ctx context.Context, identifierId, cpu, ram string) error
	AddSsh(ctx context.Context, identifierId, sshKeyIdentifier string) error
	AddScript(ctx context.Context, identifierId, scriptIdentifier string) error
	ToggleLock(ctx context.Context, identifierId string) error
	DoMultiActions(ctx context.Context, vmsIdentifiers []string, actionType, sshKeyIdentifier string) error
	EnableIpv6(ctx context.Context, identifierId string) error
	AddFip(ctx context.Context, identifierId, dcIdentifier string) error
}

type vpsieServiceHandler struct {
	client *Client
}

var _ VpsieService = &vpsieServiceHandler{}

type ListVpsieRoot struct {
	Error bool     `json:"error"`
	Data  []VmData `json:"data"`
	Total int      `json:"total"`
}

type ListVpsieByIdentifierRoot struct {
	Error bool `json:"error"`
	Data  struct {
		VmData          VmData            `json:"vmData"`
		ImageCategories []ImageCategories `json:"imageCategories"`
		VmTags          []VmTags          `json:"vmTags"`
		PrivateIpData   []PrivateIpData   `json:"privateIpData"`
		FloatingIpData  []FloatingIpData  `json:"floatingIpData"`
	} `json:"data"`
	Total int `json:"total"`
}

type ImageCategories struct {
}
type VmTags struct {
}
type PrivateIpData struct {
}
type FloatingIpData struct {
}

type VmData struct {
	ID                  int     `json:"id"`
	UserID              int     `json:"user_id"`
	BoxSizeID           int     `json:"boxsize_id"`
	BoxImageID          int     `json:"boximage_id"`
	DataCenterID        int     `json:"datacenter_id"`
	NodeID              int     `json:"node_id"`
	BoxdIsCountID       *int    `json:"boxdiscount_id"`
	Hostname            string  `json:"hostname"`
	DefaultIP           string  `json:"default_ip"`
	DefaultIPv6         string  `json:"default_ipv6"`
	PrivateIP           string  `json:"private_ip"`
	IsAutoBackup        int     `json:"is_autobackup"`
	BoxVirtualization   string  `json:"box_virtualization_id"`
	Ram                 int     `json:"ram"`
	Cpu                 int     `json:"cpu"`
	Ssd                 int     `json:"ssd"`
	Traffic             int     `json:"traffic"`
	AddedIpAddresses    *string `json:"added_ip_addresses"`
	InitialPassword     string  `json:"initial_password"`
	Notes               *string `json:"notes"`
	CreatedOn           string  `json:"created_on"`
	LastUpdated         string  `json:"last_updated"`
	DroppedOn           *string `json:"dropped_on"`
	IsActive            int     `json:"is_active"`
	IsDeleted           int     `json:"is_deleted"`
	Identifier          string  `json:"identifier"`
	Power               int     `json:"power"`
	ProjectID           int     `json:"project_id"`
	IsCustom            int     `json:"is_custom"`
	NrAddedIps          int     `json:"nr_added_ips"`
	InPcs               int     `json:"in_pcs"`
	CustomPrice         *int    `json:"custom_price"`
	PayableLicense      int     `json:"payable_license"`
	LastLicensePay      *string `json:"last_license_pay"`
	ScriptID            *int    `json:"script_id"`
	SshKeyID            *int    `json:"sshkey_id"`
	IsLocked            int     `json:"is_locked"`
	IsWorkWithNew       int     `json:"is_work_with_new_version"`
	IsSuspended         int     `json:"is_suspended"`
	IsTerminated        int     `json:"is_terminated"`
	OldID               int     `json:"old_id"`
	CustomIsoID         *int    `json:"custom_iso_id"`
	IsIsoImageBootAble  int     `json:"is_iso_image_bootable"`
	HasSsl              int     `json:"has_ssl"`
	LastActionDate      *string `json:"last_action_date,omitempty"`
	IsCreatedFromLegacy int     `json:"is_created_from_legacy"`
	IsSmtpAllowed       int     `json:"is_smtp_allowed"`
	WeeklyBackup        int     `json:"weekly_backup"`
	MonthlyBackup       int     `json:"monthly_backup"`
	LibIsoID            *int    `json:"lib_iso_id,omitempty"`
	DailySnapshot       int     `json:"daily_snapshot"`
	WeeklySnapshot      int     `json:"weekly_snapshot"`
	MonthlySnapshot     int     `json:"monthly_snap"`
	LastActionInMin     int     `json:"last_action_in_min"`
	FirstName           string  `json:"firstname"`
	LastName            string  `json:"lastname"`
	Username            string  `json:"username"`
	State               string  `json:"state"`
	IsFipAvailable      int     `json:"is_fip_available"`
	IsBucketAvailable   int     `json:"is_bucket_available"`
	DcIdentifier        string  `json:"dcIdentifier"`
	Category            string  `json:"category"`
	FullName            string  `json:"fullname"`
	VmDescription       string  `json:"vmDescription"`
	BoxesSuspended      int     `json:"boxes_suspended"`
	IsSataAvailable     int     `json:"is_sata_available"`
	IsSsdAvailable      int     `json:"is_ssd_available"`
	PublicIp            *string `json:"publicIp,omitempty"`
}

type Status struct {
	Cpu            int    `json:"cpu"`
	Ballon         int    `json:"ballon"`
	Uptime         int    `json:"uptime"`
	Pid            string `json:"pid"`
	Disk           int    `json:"disk"`
	RunningMachine string `json:"running-machine"`
	RunningQemu    string `json:"running-qemu"`
	Status         string `json:"status"`
	DiskRead       string `json:"diskread"`
	DiskWrite      string `json:"diskwrite"`
	Fullname       string `json:"fullname"`
}

type GetStatusRoot struct {
	Error  bool   `json:"error"`
	Status Status `json:"data"`
}

type VpsieConsoleRoot struct {
	Error        bool         `json:"error"`
	VpsieConsole VpsieConsole `json:"data"`
}

type VpsieConsole struct {
	Upid     string `json:"upid"`
	Ticket   string `json:"ticket"`
	Cert     string `json:"cert"`
	User     string `json:"user"`
	Port     string `json:"port"`
	Token    string `json:"token"`
	OS       string `json:"os"`
	Fullname string `json:"fullname"`
}

type CreateVpsieRequest struct {
	ResourceIdentifier string   `json:"resourceIdentifier"`
	OsIdentifier       string   `json:"osIdentifier"`
	DcIdentifier       string   `json:"dcIdentifier"`
	Hostname           string   `json:"hostname"`
	Notes              string   `json:"notes"`
	BackupEnabled      int      `json:"backupEnabled"`
	AddPublicIpV4      int      `json:"addPublicIpV4"`
	AddPublicIpV6      int      `json:"addPublicIpV6"`
	AddPrivateIp       int      `json:"addPrivateIp"`
	SshKeyIdentifier   string   `json:"sshKeyIdentifier"`
	ProjectID          int      `json:"projectId"`
	Tags               []string `json:"tags"`
}

type ActionRequest struct {
	VmIdentifier string `json:"vmIdentifier"`
}

type VpcRequest struct {
	VmIdentifier string `json:"vmIdentifier"`
	VpcId        string `json:"vpcId"`
	DcIdentifier string `json:"dcIdentifier"`
}

func (v *vpsieServiceHandler) ListVpsie(ctx context.Context, options *ListOptions, projectId string) ([]VmData, error) {
	path := fmt.Sprintf("%s?projectId=%s", vpsieBasePath, projectId)
	req, err := v.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	vpsies := new(ListVpsieRoot)
	if err = v.client.Do(ctx, req, vpsies); err != nil {
		return nil, err
	}

	return vpsies.Data, nil
}

func (v *vpsieServiceHandler) GetVpsieByIdentifier(ctx context.Context, identifierId string) (*VmData, error) {
	path := fmt.Sprintf("%s/%s", vpsieBasePath, identifierId)
	req, err := v.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	vpsies := new(ListVpsieByIdentifierRoot)
	if err = v.client.Do(ctx, req, vpsies); err != nil {
		return nil, err
	}

	return &vpsies.Data.VmData, nil
}

func (v *vpsieServiceHandler) GetVpsieStatusByIdentifier(ctx context.Context, identifierId string) (*Status, error) {
	path := fmt.Sprintf("%s/status/ %s", vpsieBasePath, identifierId)
	req, err := v.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	status := new(GetStatusRoot)
	if err = v.client.Do(ctx, req, status); err != nil {
		return nil, err
	}

	return &status.Status, nil
}

func (v *vpsieServiceHandler) GetVpsieConsole(ctx context.Context, identifierId string) (*VpsieConsole, error) {
	path := fmt.Sprintf("%s/console/%s", vpsieBasePath, identifierId)
	req, err := v.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	console := new(VpsieConsoleRoot)
	if err = v.client.Do(ctx, req, console); err != nil {
		return nil, err
	}

	return &console.VpsieConsole, nil
}

func (v *vpsieServiceHandler) CreateVpsie(ctx context.Context, vpsie *CreateVpsieRequest) error {
	req, err := v.client.NewRequest(ctx, http.MethodPost, vpsieBasePath, vpsie)
	if err != nil {
		return err
	}

	if err = v.client.Do(ctx, req, nil); err != nil {
		return err
	}

	return nil
}

func (v *vpsieServiceHandler) DeleteVpsie(ctx context.Context, identifierId string) error {
	vmIdentifier := &ActionRequest{
		VmIdentifier: identifierId,
	}
	req, err := v.client.NewRequest(ctx, http.MethodDelete, vpsieBasePath, vmIdentifier)
	if err != nil {
		return err
	}

	if err = v.client.Do(ctx, req, nil); err != nil {
		return err
	}

	return nil
}

func (v *vpsieServiceHandler) StartVpsie(ctx context.Context, identifierId string) error {
	vmIdentifier := &ActionRequest{
		VmIdentifier: identifierId,
	}

	path := fmt.Sprintf("%s/start", vpsieBasePath)
	req, err := v.client.NewRequest(ctx, http.MethodPost, path, vmIdentifier)
	if err != nil {
		return err
	}

	if err = v.client.Do(ctx, req, nil); err != nil {
		return err
	}

	return nil
}

func (v *vpsieServiceHandler) StopVpsie(ctx context.Context, identifierId string) error {
	vmIdentifier := &ActionRequest{
		VmIdentifier: identifierId,
	}

	path := fmt.Sprintf("%s/stop", vpsieBasePath)
	req, err := v.client.NewRequest(ctx, http.MethodPost, path, vmIdentifier)
	if err != nil {
		return err
	}

	if err = v.client.Do(ctx, req, nil); err != nil {
		return err
	}

	return nil
}

func (v *vpsieServiceHandler) RestartVpsie(ctx context.Context, identifierId string) error {
	vmIdentifier := &ActionRequest{
		VmIdentifier: identifierId,
	}
	path := fmt.Sprintf("%s/restart", vpsieBasePath)
	req, err := v.client.NewRequest(ctx, http.MethodPost, path, vmIdentifier)
	if err != nil {
		return err
	}

	if err = v.client.Do(ctx, req, nil); err != nil {
		return err
	}

	return nil
}

func (v *vpsieServiceHandler) ChangePassword(ctx context.Context, identifierId string, newPassword string) error {
	changePassReq := struct {
		VmIdentifier string `json:"vmIdentifier"`
		NewPassword  string `json:"newpassword"`
	}{
		VmIdentifier: identifierId,
		NewPassword:  newPassword,
	}
	path := fmt.Sprintf("%s/changepass", vpsieBasePath)
	req, err := v.client.NewRequest(ctx, http.MethodPost, path, changePassReq)
	if err != nil {
		return err
	}

	if err = v.client.Do(ctx, req, nil); err != nil {
		return err
	}

	return nil
}

func (v *vpsieServiceHandler) ChangeHostName(ctx context.Context, identifierId string, newHostname string) error {
	changeHostNameReq := struct {
		VmIdentifier string `json:"vmIdentifier"`
		Hostname     string `json:"hostname"`
	}{
		VmIdentifier: identifierId,
		Hostname:     newHostname,
	}
	path := fmt.Sprintf("%s/changehostname", vpsieBasePath)
	req, err := v.client.NewRequest(ctx, http.MethodPost, path, changeHostNameReq)
	if err != nil {
		return err
	}

	if err = v.client.Do(ctx, req, nil); err != nil {
		return err
	}

	return nil
}

func (v *vpsieServiceHandler) AddVPC(ctx context.Context, request *VpcRequest) error {
	path := fmt.Sprintf("%s/add/vpc", vpsieBasePath)

	req, err := v.client.NewRequest(ctx, http.MethodPost, path, request)
	if err != nil {
		return err
	}

	if err = v.client.Do(ctx, req, nil); err != nil {
		return err
	}

	return nil
}

func (v *vpsieServiceHandler) MoveVPC(ctx context.Context, request *VpcRequest) error {
	path := fmt.Sprintf("%s/vpc/move", vpsieBasePath)

	req, err := v.client.NewRequest(ctx, http.MethodPost, path, request)
	if err != nil {
		return err
	}

	if err = v.client.Do(ctx, req, nil); err != nil {
		return err
	}

	return nil
}

func (v *vpsieServiceHandler) ResizeVpsie(ctx context.Context, identifierId, cpu, ram string) error {
	path := fmt.Sprintf("%s/resize", vpsieBasePath)

	resizeVpsie := struct {
		VmIdentifier string `json:"vmIdentifier"`
		Ram          string `json:"ram"`
		Cpu          string `json:"cup"`
	}{
		VmIdentifier: identifierId,
		Ram:          ram,
		Cpu:          cpu,
	}

	req, err := v.client.NewRequest(ctx, http.MethodPost, path, resizeVpsie)
	if err != nil {
		return err
	}

	if err = v.client.Do(ctx, req, nil); err != nil {
		return err
	}

	return nil
}

func (v *vpsieServiceHandler) AddTags(ctx context.Context, identifierId string, tags []string) error {
	path := fmt.Sprintf("%s/addtags", vpsieBasePath)

	addTagsRequest := struct {
		VmIdentifier string   `json:"vmIdentifier"`
		Tags         []string `json:"tags"`
	}{
		VmIdentifier: identifierId,
		Tags:         tags,
	}

	req, err := v.client.NewRequest(ctx, http.MethodPost, path, addTagsRequest)
	if err != nil {
		return err
	}

	if err = v.client.Do(ctx, req, nil); err != nil {
		return err
	}

	return nil
}

func (v *vpsieServiceHandler) AddSsh(ctx context.Context, identifierId, sshKeyIdentifier string) error {
	path := fmt.Sprintf("%s/sshkey", vpsieBasePath)
	addSshReq := struct {
		VmIdentifier     string `json:"vmIdentifier"`
		SshKeyIdentifier string `json:"sshKeyIdentifier"`
	}{
		VmIdentifier:     identifierId,
		SshKeyIdentifier: sshKeyIdentifier,
	}

	req, err := v.client.NewRequest(ctx, http.MethodPost, path, addSshReq)
	if err != nil {
		return err
	}

	if err = v.client.Do(ctx, req, nil); err != nil {
		return err
	}

	return nil
}

func (v *vpsieServiceHandler) AddScript(ctx context.Context, identifierId, scriptIdentifier string) error {
	path := fmt.Sprintf("%s/script", vpsieBasePath)
	addScriptReq := struct {
		VmIdentifier     string `json:"vmIdentifier"`
		ScriptIdentifier string `json:"scriptIdentifier"`
	}{
		VmIdentifier:     identifierId,
		ScriptIdentifier: scriptIdentifier,
	}

	req, err := v.client.NewRequest(ctx, http.MethodPost, path, addScriptReq)
	if err != nil {
		return err
	}

	if err = v.client.Do(ctx, req, nil); err != nil {
		return err
	}

	return nil
}

func (v *vpsieServiceHandler) ToggleLock(ctx context.Context, identifierId string) error {
	path := fmt.Sprintf("%s/toggle/lock", vpsieBasePath)
	addScriptReq := struct {
		VmIdentifier string `json:"vmIdentifier"`
	}{
		VmIdentifier: identifierId,
	}

	req, err := v.client.NewRequest(ctx, http.MethodPost, path, addScriptReq)
	if err != nil {
		return err
	}

	if err = v.client.Do(ctx, req, nil); err != nil {
		return err
	}

	return nil
}

func (v *vpsieServiceHandler) DoMultiActions(ctx context.Context, vmsIdentifiers []string, actionType, sshKeyIdentifier string) error {
	path := fmt.Sprintf("%s/actions", vpsieBasePath)
	addScriptReq := struct {
		VmsIdentifiers   []string `json:"vmsIdentifiers"`
		ActionType       string   `json:"actionType"`
		SshKeyIdentifier string   `json:"sshKeyIdentifier"`
	}{
		VmsIdentifiers:   vmsIdentifiers,
		ActionType:       actionType,
		SshKeyIdentifier: sshKeyIdentifier,
	}

	req, err := v.client.NewRequest(ctx, http.MethodPost, path, addScriptReq)
	if err != nil {
		return err
	}

	if err = v.client.Do(ctx, req, nil); err != nil {
		return err
	}

	return nil
}

func (v *vpsieServiceHandler) AddFip(ctx context.Context, identifierId, dcIdentifier string) error {
	path := fmt.Sprintf("%s/fip/add", vpsieBasePath)
	addScriptReq := struct {
		VmIdentifier string `json:"vmIdentifier"`
		DcIdentifier string `json:"dcIdentifier"`
	}{
		VmIdentifier: identifierId,
		DcIdentifier: dcIdentifier,
	}

	req, err := v.client.NewRequest(ctx, http.MethodPost, path, addScriptReq)
	if err != nil {
		return err
	}

	if err = v.client.Do(ctx, req, nil); err != nil {
		return err
	}

	return nil
}

func (v *vpsieServiceHandler) EnableIpv6(ctx context.Context, identifierId string) error {
	path := fmt.Sprintf("%s/script", vpsieBasePath)
	addScriptReq := struct {
		VmIdentifier string `json:"vmIdentifier"`
	}{
		VmIdentifier: identifierId,
	}

	req, err := v.client.NewRequest(ctx, http.MethodPost, path, addScriptReq)
	if err != nil {
		return err
	}

	if err = v.client.Do(ctx, req, nil); err != nil {
		return err
	}

	return nil
}
