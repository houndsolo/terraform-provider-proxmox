resource "proxmox_network_linux_eth" "example" {
  name      = "eno1"
  node_name = "pve"

  autostart = false
  comment   = "Managed by OpenTofu"
  mtu       = 1500
}
