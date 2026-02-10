# VMware Inventory

CLI tool that connects to VMware vCenter and exports ESXi host hardware inventory to CSV, including CPU, memory, and vSAN disk capacity.

## Install

Download the latest binary for your platform from the [Releases](https://github.com/carahsoft/VMware-Inventory/releases) page.

## Usage

```sh
# Windows
.\vmware-inventory-windows-amd64.exe -host <vcenter> -user <username>

# macOS
./vmware-inventory-mac-arm64 -host <vcenter> -user <username>

# Linux
./vmware-inventory-linux-amd64 -host <vcenter> -user <username>
```

If `-password` is not provided, you will be prompted securely (input hidden).

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-host` | *(required)* | vCenter hostname or IP |
| `-user` | *(required)* | vCenter username |
| `-password` | *(prompted)* | vCenter password; prompted if omitted |
| `-output` | `hosts_cpu.csv` | Output CSV file path |
| `-insecure` | `true` | Allow self-signed TLS certificates |
| `-anonymize` | `false` | Replace hostnames with generic names (Host 1, Host 2, ...) |

### Examples

```sh
# Windows
.\vmware-inventory-windows-amd64.exe -host vcenter.example.com -user administrator@vsphere.local
.\vmware-inventory-windows-amd64.exe -host vcenter.example.com -user administrator@vsphere.local -password secret -output inventory.csv
.\vmware-inventory-windows-amd64.exe -host vcenter.example.com -user administrator@vsphere.local -anonymize

# macOS
./vmware-inventory-mac-arm64 -host vcenter.example.com -user administrator@vsphere.local

# Linux
./vmware-inventory-linux-amd64 -host vcenter.example.com -user administrator@vsphere.local
```

## Output

Produces a CSV file with the following columns:

| Column | Description |
|--------|-------------|
| Hostname | ESXi host name (or generic name when `-anonymize` is used) |
| Cluster | vCenter cluster name (or generic name when `-anonymize` is used) |
| Server Model | Hardware server model |
| ESXi Version | ESXi version number |
| CPU Model | Processor model (from first CPU package) |
| Socket Count | Number of physical CPU sockets |
| Cores per Socket | CPU cores per socket |
| Total Cores | Total physical cores across all sockets |
| Memory GB | Total physical memory in GB |
| vSAN Type | vSAN cluster architecture: OSA or ESA (empty if not vSAN) |
| vSAN Capacity Disks | Number of vSAN capacity-tier disks (excludes cache disks) |
| vSAN Cache Disks | Number of vSAN cache-tier disks (0 for ESA) |
| vSAN Capacity TiB | Total raw capacity of vSAN capacity disks in TiB (excludes cache) |

A summary line is printed to stderr:

```
Wrote 12 hosts to hosts_cpu.csv
```

## Build from source

```sh
go build -o vmware-inventory
```
