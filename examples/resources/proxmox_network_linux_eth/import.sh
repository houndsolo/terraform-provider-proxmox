#!/usr/bin/env sh
# Linux Ethernet interfaces can be imported using the node name and interface name
# in the format `<node name>:<iface>`.
tofu import proxmox_network_linux_eth.example pve:eno1
