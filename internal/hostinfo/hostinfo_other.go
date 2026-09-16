//go:build !windows

package hostinfo

import (
	"os"
	"os/user"
	"runtime"
	"time"

	"femucaribe-backup-agent/internal/version"
)

// CollectHostSpecs es la implementación fallback para sistemas no Windows.
func CollectHostSpecs() HostSpecs {
	hName, _ := os.Hostname()
	netInfo := GetNetworkInfo()

	return HostSpecs{
		HostID:           GenerateHostID(hName, netInfo.PrimaryMAC),
		Hostname:         hName,
		DomainName:       "LOCAL",
		OSName:           runtime.GOOS,
		OSFamily:         runtime.GOOS,
		OSVersion:        runtime.GOOS,
		OSBuild:          "unknown",
		OSArch:           runtime.GOARCH,
		CPUModel:         "Generic CPU",
		CPUCoresLogical:  runtime.NumCPU(),
		CPUCoresPhysical: runtime.NumCPU(),
		TotalRAMBytes:    16 * 1024 * 1024 * 1024, // 16 GB fallback
		MACAddresses:     netInfo.MACAddresses,
		PrimaryMAC:       netInfo.PrimaryMAC,
	}
}

// CollectRuntimeSnapshot es la implementación fallback para sistemas no Windows.
func CollectRuntimeSnapshot(backupDir string) RuntimeSnapshot {
	netInfo := GetNetworkInfo()
	procPath, _ := os.Executable()
	pid := os.Getpid()

	uName := os.Getenv("USER")
	uDomain := ""
	if cu, err := user.Current(); err == nil && cu != nil {
		uName = cu.Username
	}

	return RuntimeSnapshot{
		PrimaryIP:            netInfo.PrimaryIP,
		LocalIPs:             netInfo.LocalIPs,
		Username:             uName,
		UserDomain:           uDomain,
		IsElevatedAdmin:      os.Geteuid() == 0,
		ProcessID:            pid,
		ProcessPath:          procPath,
		AgentVersion:         version.Current,
		GoVersion:            runtime.Version(),
		FreeRAMBytes:         8 * 1024 * 1024 * 1024,
		BackupDiskDrive:      "/",
		BackupDiskFreeBytes:  50 * 1024 * 1024 * 1024,
		BackupDiskTotalBytes: 100 * 1024 * 1024 * 1024,
		SystemUptimeSeconds:  3600,
		Timezone:             time.Now().Location().String(),
	}
}
