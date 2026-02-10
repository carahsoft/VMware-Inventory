package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"syscall"

	"golang.org/x/term"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

func main() {
	host := flag.String("host", "", "vCenter hostname or IP (required)")
	user := flag.String("user", "", "vCenter username (required)")
	password := flag.String("password", "", "vCenter password (prompted if not provided)")
	output := flag.String("output", "hosts_cpu.csv", "output CSV file path")
	insecure := flag.Bool("insecure", true, "allow self-signed TLS certificates")
	anonymize := flag.Bool("anonymize", false, "omit hostnames from CSV output")
	debug := flag.Bool("debug", false, "print raw vSAN config JSON per host to stderr")
	flag.Parse()

	if *host == "" || *user == "" {
		flag.Usage()
		os.Exit(1)
	}

	if *password == "" {
		fmt.Fprint(os.Stderr, "Password: ")
		b, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			log.Fatalf("Error reading password: %v", err)
		}
		*password = string(b)
	}

	ctx := context.Background()

	// Build vCenter SDK URL
	u, err := url.Parse(fmt.Sprintf("https://%s/sdk", *host))
	if err != nil {
		log.Fatalf("Error parsing URL: %v", err)
	}
	u.User = url.UserPassword(*user, *password)

	// Connect and login
	client, err := govmomi.NewClient(ctx, u, *insecure)
	if err != nil {
		log.Fatalf("Error connecting to vCenter: %v", err)
	}
	defer client.Logout(ctx)

	// Create a container view of all HostSystem objects
	m := view.NewManager(client.Client)
	v, err := m.CreateContainerView(ctx, client.ServiceContent.RootFolder, []string{"HostSystem"}, true)
	if err != nil {
		log.Fatalf("Error creating container view: %v", err)
	}
	defer v.Destroy(ctx)

	// Retrieve host summary, hardware, and configManager properties
	var hosts []mo.HostSystem
	err = v.Retrieve(ctx, []string{"HostSystem"}, []string{"summary", "hardware", "configManager", "parent"}, &hosts)
	if err != nil {
		log.Fatalf("Error retrieving hosts: %v", err)
	}

	pc := property.DefaultCollector(client.Client)

	// Retrieve cluster/parent names for hosts
	parentNames := make(map[string]string) // parent MoRef Value -> name
	for _, h := range hosts {
		if h.Parent == nil {
			continue
		}
		if _, ok := parentNames[h.Parent.Value]; ok {
			continue
		}
		var parent mo.ManagedEntity
		if err := pc.RetrieveOne(ctx, *h.Parent, []string{"name"}, &parent); err != nil {
			log.Printf("Warning: could not retrieve cluster name for %s: %v", h.Summary.Config.Name, err)
			continue
		}
		parentNames[h.Parent.Value] = parent.Name
	}

	// Build anonymized cluster name mapping
	anonClusters := make(map[string]string)
	if *anonymize {
		idx := 0
		for _, h := range hosts {
			if h.Parent == nil {
				continue
			}
			realName := parentNames[h.Parent.Value]
			if _, ok := anonClusters[realName]; !ok {
				idx++
				anonClusters[realName] = fmt.Sprintf("Cluster %d", idx)
			}
		}
	}

	// Retrieve vSAN disk info per host
	type vsanHostInfo struct {
		capacityTiB float64
		totalDisks  int
		cacheDisks  int
		clusterType string // "OSA" or "ESA"
	}
	vsanInfo := make(map[string]vsanHostInfo)
	for _, h := range hosts {
		vsanRef := h.ConfigManager.VsanSystem
		if vsanRef == nil {
			continue
		}
		var vsanSys mo.HostVsanSystem
		err = pc.RetrieveOne(ctx, *vsanRef, nil, &vsanSys)
		if err != nil {
			log.Printf("Warning: could not retrieve vSAN config for %s: %v", h.Summary.Config.Name, err)
			continue
		}
		if *debug {
			j, _ := json.MarshalIndent(vsanSys, "", "  ")
			fmt.Printf("=== vSAN system for %s ===\n%s\n\n", h.Summary.Config.Name, j)
		}

		isESA := vsanSys.Config.VsanEsaEnabled != nil && *vsanSys.Config.VsanEsaEnabled

		var info vsanHostInfo
		var capacityBytes int64

		if isESA {
			// ESA: no disk groups, query disks directly
			info.clusterType = "ESA"
			res, err := methods.QueryDisksForVsan(ctx, client.Client, &types.QueryDisksForVsan{
				This: *vsanRef,
			})
			if err != nil {
				log.Printf("Warning: could not query vSAN disks for %s: %v", h.Summary.Config.Name, err)
			} else {
				if *debug {
					j, _ := json.MarshalIndent(res.Returnval, "", "  ")
					fmt.Printf("=== vSAN disks for %s ===\n%s\n\n", h.Summary.Config.Name, j)
				}
				for _, dr := range res.Returnval {
					// For ESA, disks in use have vsanDiskInfo populated
					inUse := dr.Disk.VsanDiskInfo != nil
					if inUse {
						info.totalDisks++
						capacityBytes += int64(dr.Disk.Capacity.BlockSize) * int64(dr.Disk.Capacity.Block)
					}
				}
			}
		} else {
			// OSA: disk groups with cache SSD + capacity disks
			if vsanSys.Config.StorageInfo == nil || len(vsanSys.Config.StorageInfo.DiskMapping) == 0 {
				continue
			}
			info.clusterType = "OSA"
			info.cacheDisks = len(vsanSys.Config.StorageInfo.DiskMapping)
			for _, dm := range vsanSys.Config.StorageInfo.DiskMapping {
				info.totalDisks += len(dm.NonSsd)
				for _, d := range dm.NonSsd {
					capacityBytes += int64(d.Capacity.BlockSize) * int64(d.Capacity.Block)
				}
			}
		}

		info.capacityTiB = float64(capacityBytes) / (1024 * 1024 * 1024 * 1024)
		vsanInfo[h.Summary.Config.Name] = info
	}

	// Write CSV
	f, err := os.Create(*output)
	if err != nil {
		log.Fatalf("Error creating output file: %v", err)
	}

	w := csv.NewWriter(f)

	w.Write([]string{"Hostname", "Cluster", "Server Model", "ESXi Version", "CPU Model", "Socket Count", "Cores per Socket", "Total Cores", "Memory GB", "vSAN Type", "vSAN Capacity Disks", "vSAN Cache Disks", "vSAN Capacity TiB"})

	for i, h := range hosts {
		hostname := h.Summary.Config.Name
		if *anonymize {
			hostname = fmt.Sprintf("Host %d", i+1)
		}

		cluster := ""
		if h.Parent != nil {
			cluster = parentNames[h.Parent.Value]
			if *anonymize {
				cluster = anonClusters[cluster]
			}
		}

		serverModel := ""
		if h.Summary.Hardware != nil {
			serverModel = h.Summary.Hardware.Model
		}

		esxiVersion := ""
		if h.Summary.Config.Product != nil {
			esxiVersion = h.Summary.Config.Product.Version
		}

		cpuModel := ""
		if h.Hardware != nil && len(h.Hardware.CpuPkg) > 0 {
			cpuModel = h.Hardware.CpuPkg[0].Description
		}

		var sockets, totalCores, coresPerSocket int16
		var memoryGB int64
		if h.Hardware != nil {
			sockets = h.Hardware.CpuInfo.NumCpuPackages
			totalCores = h.Hardware.CpuInfo.NumCpuCores
			if sockets > 0 {
				coresPerSocket = totalCores / sockets
			}
			memoryGB = h.Hardware.MemorySize / (1024 * 1024 * 1024)
		}

		info := vsanInfo[h.Summary.Config.Name]

		w.Write([]string{
			hostname,
			cluster,
			serverModel,
			esxiVersion,
			cpuModel,
			strconv.Itoa(int(sockets)),
			strconv.Itoa(int(coresPerSocket)),
			strconv.Itoa(int(totalCores)),
			strconv.FormatInt(memoryGB, 10),
			info.clusterType,
			strconv.Itoa(info.totalDisks),
			strconv.Itoa(info.cacheDisks),
			fmt.Sprintf("%.1f", info.capacityTiB),
		})
	}

	w.Flush()
	if err := w.Error(); err != nil {
		log.Fatalf("Error writing CSV: %v", err)
	}
	if err := f.Close(); err != nil {
		log.Fatalf("Error closing output file: %v", err)
	}

	fmt.Fprintf(os.Stderr, "Wrote %d hosts to %s\n", len(hosts), *output)
}
