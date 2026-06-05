/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package network

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/bpg/terraform-provider-proxmox/fwprovider/attribute"
	"github.com/bpg/terraform-provider-proxmox/fwprovider/config"
	customtypes "github.com/bpg/terraform-provider-proxmox/fwprovider/types"
	"github.com/bpg/terraform-provider-proxmox/proxmox"
	"github.com/bpg/terraform-provider-proxmox/proxmox/nodes"
	proxmoxtypes "github.com/bpg/terraform-provider-proxmox/proxmox/types"
)

var (
	_ resource.Resource                = &linuxEthResource{}
	_ resource.ResourceWithConfigure   = &linuxEthResource{}
	_ resource.ResourceWithImportState = &linuxEthResource{}
)

type linuxEthResourceModel struct {
	ID        types.String            `tfsdk:"id"`
	NodeName  types.String            `tfsdk:"node_name"`
	Name      types.String            `tfsdk:"name"`
	Address   customtypes.IPCIDRValue `tfsdk:"address"`
	Gateway   customtypes.IPAddrValue `tfsdk:"gateway"`
	Address6  customtypes.IPCIDRValue `tfsdk:"address6"`
	Gateway6  customtypes.IPAddrValue `tfsdk:"gateway6"`
	Autostart types.Bool              `tfsdk:"autostart"`
	MTU       types.Int64             `tfsdk:"mtu"`
	Comment   types.String            `tfsdk:"comment"`
	Timeout   types.Int64             `tfsdk:"timeout_reload"`
}

func (m *linuxEthResourceModel) exportToNetworkInterfaceCreateUpdateBody() *nodes.NetworkInterfaceCreateUpdateRequestBody {
	body := &nodes.NetworkInterfaceCreateUpdateRequestBody{
		Iface:     m.Name.ValueString(),
		Type:      "eth",
		Autostart: proxmoxtypes.CustomBool(m.Autostart.ValueBool()).Pointer(),
	}

	body.CIDR = m.Address.ValueStringPointer()
	body.Gateway = m.Gateway.ValueStringPointer()
	body.CIDR6 = m.Address6.ValueStringPointer()
	body.Gateway6 = m.Gateway6.ValueStringPointer()
	body.Comments = attribute.StringPtrFromValue(m.Comment)
	body.MTU = attribute.Int64PtrFromValue(m.MTU)

	return body
}

func (m *linuxEthResourceModel) importFromNetworkInterfaceList(iface *nodes.NetworkInterfaceListResponseData) {
	m.Address = customtypes.NewIPCIDRPointerValue(iface.CIDR)
	m.Gateway = customtypes.NewIPAddrPointerValue(iface.Gateway)
	m.Address6 = customtypes.NewIPCIDRPointerValue(iface.CIDR6)
	m.Gateway6 = customtypes.NewIPAddrPointerValue(iface.Gateway6)

	m.Autostart = types.BoolPointerValue(iface.Autostart.PointerBool())
	if m.Autostart.IsNull() {
		m.Autostart = types.BoolValue(false)
	}

	if iface.MTU != nil {
		if v, err := strconv.Atoi(*iface.MTU); err == nil {
			m.MTU = types.Int64Value(int64(v))
		} else {
			m.MTU = types.Int64Null()
		}
	} else {
		m.MTU = types.Int64Null()
	}

	if iface.Comments != nil {
		m.Comment = types.StringValue(strings.TrimSpace(*iface.Comments))
	}
}

// NewLinuxEthResource creates a new resource for managing Linux Ethernet network interfaces.
func NewLinuxEthResource() resource.Resource {
	return &linuxEthResource{}
}

type linuxEthResource struct {
	client proxmox.Client
}

func (r *linuxEthResource) Metadata(
	_ context.Context,
	_ resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = "proxmox_network_linux_eth"
}

// Schema defines the schema for the resource.
func (r *linuxEthResource) Schema(
	_ context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = schema.Schema{
		Description: "Manages a Linux Ethernet network interface in a Proxmox VE node.",
		Attributes: map[string]schema.Attribute{
			"id": attribute.ResourceID("A unique identifier with format `<node name>:<iface>`."),
			"node_name": schema.StringAttribute{
				Description: "The name of the node.",
				Required:    true,
			},
			"name": schema.StringAttribute{
				Description: "The physical Ethernet interface name.",
				MarkdownDescription: "The physical Ethernet interface name, such as `eno1`, `ens18`, `enp3s0f1`, or `eth0`. " +
					"The interface must already exist on the Proxmox VE node.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(2),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"address": schema.StringAttribute{
				Description: "The interface IPv4/CIDR address.",
				CustomType:  customtypes.IPCIDRType{},
				Optional:    true,
			},
			"gateway": schema.StringAttribute{
				Description: "Default gateway address.",
				CustomType:  customtypes.IPAddrType{},
				Optional:    true,
			},
			"address6": schema.StringAttribute{
				Description: "The interface IPv6/CIDR address.",
				CustomType:  customtypes.IPCIDRType{},
				Optional:    true,
			},
			"gateway6": schema.StringAttribute{
				Description: "Default IPv6 gateway address.",
				CustomType:  customtypes.IPAddrType{},
				Optional:    true,
			},
			"autostart": schema.BoolAttribute{
				Description: "Automatically start interface on boot (defaults to `true`).",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
			},
			"mtu": schema.Int64Attribute{
				Description: "The interface MTU.",
				Optional:    true,
			},
			"comment": schema.StringAttribute{
				Description: "Comment for the interface.",
				Optional:    true,
			},
			"timeout_reload": schema.Int64Attribute{
				Description: "Timeout for network reload operations in seconds (defaults to `100`).",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(int64(nodes.NetworkReloadTimeout.Seconds())),
				Validators: []validator.Int64{
					int64validator.AtLeast(5),
				},
			},
		},
	}
}

func (r *linuxEthResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	if req.ProviderData == nil {
		return
	}

	cfg, ok := req.ProviderData.(config.Resource)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected config.Resource, got: %T", req.ProviderData),
		)

		return
	}

	r.client = cfg.Client
}

//nolint:dupl // Lifecycle methods intentionally mirror other node network resources.
func (r *linuxEthResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan linuxEthResourceModel

	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)

	if resp.Diagnostics.HasError() {
		return
	}

	probe := linuxEthResourceModel{
		NodeName: plan.NodeName,
		Name:     plan.Name,
	}
	if !r.read(ctx, &probe, &resp.Diagnostics) {
		if resp.Diagnostics.HasError() {
			return
		}

		resp.Diagnostics.AddError(
			"Linux Ethernet interface not found",
			fmt.Sprintf("Interface %q on node %q must already exist", plan.Name.ValueString(), plan.NodeName.ValueString()),
		)

		return
	}

	body := plan.exportToNetworkInterfaceCreateUpdateBody()

	err := r.client.Node(plan.NodeName.ValueString()).UpdateNetworkInterface(ctx, plan.Name.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error updating Linux Ethernet interface",
			"Could not update Linux Ethernet, unexpected error: "+err.Error(),
		)

		return
	}

	plan.ID = types.StringValue(plan.NodeName.ValueString() + ":" + plan.Name.ValueString())

	found := r.read(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	if !found {
		resp.Diagnostics.AddError(
			"Linux Ethernet interface not found after update",
			fmt.Sprintf(
				"Interface %q on node %q could not be read after update",
				plan.Name.ValueString(), plan.NodeName.ValueString()),
		)

		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)

	reloadCtx, cancel := context.WithTimeout(ctx, time.Duration(plan.Timeout.ValueInt64())*time.Second)
	defer cancel()

	err = r.client.Node(plan.NodeName.ValueString()).ReloadNetworkConfiguration(reloadCtx)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error reloading network configuration",
			fmt.Sprintf("Could not reload network configuration on node '%s', unexpected error: %s",
				plan.NodeName.ValueString(), err.Error()),
		)
	}
}

func (r *linuxEthResource) read(ctx context.Context, model *linuxEthResourceModel, diags *diag.Diagnostics) bool {
	ifaces, err := r.client.Node(model.NodeName.ValueString()).ListNetworkInterfaces(ctx)
	if err != nil {
		diags.AddError(
			"Error listing network interfaces",
			"Could not list network interfaces, unexpected error: "+err.Error(),
		)

		return false
	}

	for _, iface := range ifaces {
		if iface.Iface != model.Name.ValueString() || iface.Type != "eth" {
			continue
		}

		model.importFromNetworkInterfaceList(iface)

		return true
	}

	return false
}

// Read reads a Linux Ethernet interface.
//
//nolint:dupl // Lifecycle methods intentionally mirror other node network resources.
func (r *linuxEthResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state linuxEthResourceModel

	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)

	if resp.Diagnostics.HasError() {
		return
	}

	found := r.read(ctx, &state, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update updates a Linux Ethernet interface.
//
//nolint:dupl // Lifecycle methods intentionally mirror other node network resources.
func (r *linuxEthResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state linuxEthResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	body := plan.exportToNetworkInterfaceCreateUpdateBody()

	var toDelete []string

	attribute.CheckDelete(plan.Address, state.Address, &toDelete, "cidr")
	attribute.CheckDelete(plan.Address6, state.Address6, &toDelete, "cidr6")
	attribute.CheckDelete(plan.MTU, state.MTU, &toDelete, "mtu")
	attribute.CheckDelete(plan.Gateway, state.Gateway, &toDelete, "gateway")
	attribute.CheckDelete(plan.Gateway6, state.Gateway6, &toDelete, "gateway6")
	attribute.CheckDelete(plan.Comment, state.Comment, &toDelete, "comments")

	if len(toDelete) > 0 {
		body.Delete = toDelete
	}

	err := r.client.Node(plan.NodeName.ValueString()).UpdateNetworkInterface(ctx, plan.Name.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error updating Linux Ethernet interface",
			"Could not update Linux Ethernet, unexpected error: "+err.Error(),
		)

		return
	}

	found := r.read(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	if !found {
		resp.Diagnostics.AddError(
			"Linux Ethernet interface not found after update",
			fmt.Sprintf(
				"Interface %q on node %q could not be read after update",
				plan.Name.ValueString(), plan.NodeName.ValueString()),
		)

		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)

	reloadCtx, cancel := context.WithTimeout(ctx, time.Duration(plan.Timeout.ValueInt64())*time.Second)
	defer cancel()

	err = r.client.Node(plan.NodeName.ValueString()).ReloadNetworkConfiguration(reloadCtx)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error reloading network configuration",
			fmt.Sprintf("Could not reload network configuration on node '%s', unexpected error: %s",
				plan.NodeName.ValueString(), err.Error()),
		)
	}
}

// Delete deletes a Linux Ethernet interface configuration.
//
//nolint:dupl // Lifecycle methods intentionally mirror other node network resources.
func (r *linuxEthResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state linuxEthResourceModel

	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)

	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.Node(state.NodeName.ValueString()).DeleteNetworkInterface(ctx, state.Name.ValueString())
	if err != nil {
		if strings.Contains(err.Error(), "interface does not exist") {
			resp.Diagnostics.AddWarning(
				"Linux Ethernet interface does not exist",
				fmt.Sprintf("Could not delete Linux Ethernet '%s', interface does not exist, "+
					"or has already been deleted outside of Terraform.", state.Name.ValueString()),
			)
		} else {
			resp.Diagnostics.AddError(
				"Error deleting Linux Ethernet interface",
				fmt.Sprintf("Could not delete Linux Ethernet '%s', unexpected error: %s",
					state.Name.ValueString(), err.Error()),
			)
		}

		return
	}

	reloadCtx, cancel := context.WithTimeout(ctx, time.Duration(state.Timeout.ValueInt64())*time.Second)
	defer cancel()

	err = r.client.Node(state.NodeName.ValueString()).ReloadNetworkConfiguration(reloadCtx)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error reloading network configuration",
			fmt.Sprintf("Could not reload network configuration on node '%s', unexpected error: %s",
				state.NodeName.ValueString(), err.Error()),
		)
	}
}

//nolint:dupl // ImportState mirrors other network resources but is bound to a distinct resource type.
func (r *linuxEthResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	idParts := strings.Split(req.ID, ":")
	if len(idParts) != 2 || idParts[0] == "" || idParts[1] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: `node_name:iface`. Got: %q", req.ID),
		)

		return
	}

	nodeName := idParts[0]
	iface := idParts[1]

	state := linuxEthResourceModel{
		ID:       types.StringValue(req.ID),
		NodeName: types.StringValue(nodeName),
		Name:     types.StringValue(iface),
		Timeout:  types.Int64Value(int64(nodes.NetworkReloadTimeout.Seconds())),
	}
	found := r.read(ctx, &state, &resp.Diagnostics)

	if resp.Diagnostics.HasError() {
		return
	}

	if !found {
		resp.Diagnostics.AddError(
			"Linux Ethernet interface not found",
			fmt.Sprintf("Interface %q on node %q could not be imported", iface, nodeName),
		)

		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}
