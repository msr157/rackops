// Package hwinfo reads hardware and OS facts from the local machine using
// gopsutil, normalized into the shared api.Snapshot wire type.
package hwinfo

import (
	"context"
	"runtime"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	gnet "github.com/shirou/gopsutil/v4/net"

	"github.com/msr157/rackops/internal/api"
)

// Collect gathers a full snapshot. Individual probes that fail leave their
// section zero-valued rather than failing the whole snapshot; only a total
// host-info failure returns an error. The CPU usage probe blocks for ~1s.
func Collect(ctx context.Context) (api.Snapshot, error) {
	snap := api.Snapshot{
		Arch:        runtime.GOARCH,
		CollectedAt: time.Now().UTC(),
	}

	info, err := host.InfoWithContext(ctx)
	if err != nil {
		return snap, err
	}
	snap.Hostname = info.Hostname
	snap.OS = info.OS
	snap.Platform = strings.TrimSpace(info.Platform + " " + info.PlatformVersion)
	snap.UptimeSec = info.Uptime

	snap.CPU = collectCPU(ctx)
	snap.Memory = collectMemory(ctx)
	snap.Disks = collectDisks(ctx)
	snap.Network = collectNetwork(ctx)

	return snap, nil
}

func collectCPU(ctx context.Context) api.CPUInfo {
	out := api.CPUInfo{Cores: runtime.NumCPU()}
	if infos, err := cpu.InfoWithContext(ctx); err == nil && len(infos) > 0 {
		out.Model = strings.TrimSpace(infos[0].ModelName)
	}
	// Blocking one-second sample of overall usage.
	if pcts, err := cpu.PercentWithContext(ctx, time.Second, false); err == nil && len(pcts) > 0 {
		out.UsagePct = pcts[0]
	}
	return out
}

func collectMemory(ctx context.Context) api.MemInfo {
	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return api.MemInfo{}
	}
	return api.MemInfo{
		TotalBytes: vm.Total,
		UsedBytes:  vm.Used,
		UsedPct:    vm.UsedPercent,
	}
}

func collectDisks(ctx context.Context) []api.DiskInfo {
	parts, err := disk.PartitionsWithContext(ctx, false)
	if err != nil {
		return nil
	}
	var disks []api.DiskInfo
	for _, p := range parts {
		usage, err := disk.UsageWithContext(ctx, p.Mountpoint)
		if err != nil || usage.Total == 0 {
			continue // skip virtual/unreadable mounts (e.g. empty CD drives)
		}
		disks = append(disks, api.DiskInfo{
			Mountpoint: p.Mountpoint,
			FS:         p.Fstype,
			TotalBytes: usage.Total,
			UsedBytes:  usage.Used,
			UsedPct:    usage.UsedPercent,
		})
	}
	return disks
}

func collectNetwork(ctx context.Context) []api.NetIface {
	ifaces, err := gnet.InterfacesWithContext(ctx)
	if err != nil {
		return nil
	}
	var out []api.NetIface
	for _, iface := range ifaces {
		up, loopback := false, false
		for _, flag := range iface.Flags {
			switch flag {
			case "up":
				up = true
			case "loopback":
				loopback = true
			}
		}
		if !up || loopback {
			continue
		}
		for _, addr := range iface.Addrs {
			ip := strings.Split(addr.Addr, "/")[0]
			if strings.Contains(ip, ":") {
				continue // IPv4 only for the MVP
			}
			out = append(out, api.NetIface{Name: iface.Name, IPv4: ip})
			break
		}
	}
	return out
}
