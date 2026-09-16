//go:build windows

package hostinfo

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"femucaribe-backup-agent/internal/version"
)

type memoryStatusEx struct {
	cbSize                  uint32
	dwMemoryLoad            uint32
	ullTotalPhys            uint64
	ullAvailPhys            uint64
	ullTotalPageFile        uint64
	ullAvailPageFile        uint64
	ullTotalVirtual         uint64
	ullAvailVirtual         uint64
	ullAvailExtendedVirtual uint64
}

var (
	kernel32              = windows.NewLazySystemDLL("kernel32.dll")
	procGlobalMemoryStatus = kernel32.NewProc("GlobalMemoryStatusEx")
	procGetTickCount64     = kernel32.NewProc("GetTickCount64")
)

// CollectHostSpecs consulta las especificaciones físicas del hardware y SO en Windows.
func CollectHostSpecs() HostSpecs {
	hName, _ := os.Hostname()
	netInfo := GetNetworkInfo()

	specs := HostSpecs{
		Hostname:         hName,
		OSFamily:         "windows",
		OSArch:           runtime.GOARCH,
		CPUCoresLogical:  runtime.NumCPU(),
		CPUCoresPhysical: runtime.NumCPU(),
		MACAddresses:     netInfo.MACAddresses,
		PrimaryMAC:       netInfo.PrimaryMAC,
	}
	specs.HostID = GenerateHostID(hName, netInfo.PrimaryMAC)

	// Dominio de Windows
	domain := os.Getenv("USERDOMAIN")
	if domain != "" && !strings.EqualFold(domain, hName) {
		specs.DomainName = domain
	} else {
		specs.DomainName = "WORKGROUP"
	}

	// Información del Sistema Operativo desde el Registro de Windows
	specs.OSName = "Windows"
	specs.OSVersion = runtime.GOOS
	specs.OSBuild = "Unknown"

	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE)
	if err == nil {
		defer k.Close()
		if prodName, _, err := k.GetStringValue("ProductName"); err == nil && prodName != "" {
			specs.OSName = prodName
		}
		if dispVer, _, err := k.GetStringValue("DisplayVersion"); err == nil && dispVer != "" {
			specs.OSVersion = dispVer
		}
		if build, _, err := k.GetStringValue("CurrentBuild"); err == nil && build != "" {
			if ubr, _, err := k.GetIntegerValue("UBR"); err == nil {
				specs.OSBuild = build + "." + string(rune(ubr))
			} else {
				specs.OSBuild = build
			}
		}
	}

	// Modelo de CPU desde el Registro
	kCPU, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\CentralProcessor\0`, registry.QUERY_VALUE)
	if err == nil {
		defer kCPU.Close()
		if cpuName, _, err := kCPU.GetStringValue("ProcessorNameString"); err == nil {
			specs.CPUModel = strings.TrimSpace(cpuName)
		}
	}

	// Memoria RAM total
	var mem memoryStatusEx
	mem.cbSize = uint32(unsafe.Sizeof(mem))
	r1, _, _ := procGlobalMemoryStatus.Call(uintptr(unsafe.Pointer(&mem)))
	if r1 != 0 {
		specs.TotalRAMBytes = int64(mem.ullTotalPhys)
	}

	return specs
}

// CollectRuntimeSnapshot toma la foto en vivo del proceso, recursos y red en Windows.
func CollectRuntimeSnapshot(backupDir string) RuntimeSnapshot {
	netInfo := GetNetworkInfo()
	procPath, _ := os.Executable()
	pid := os.Getpid()

	snap := RuntimeSnapshot{
		PrimaryIP:    netInfo.PrimaryIP,
		LocalIPs:     netInfo.LocalIPs,
		ProcessID:    pid,
		ProcessPath:  procPath,
		AgentVersion: version.Current,
		GoVersion:    runtime.Version(),
		Timezone:     time.Now().Location().String(),
	}

	// Usuario y Privilegios de Administrador
	currentUser, err := user.Current()
	if err == nil && currentUser != nil {
		snap.Username = currentUser.Username
	} else {
		snap.Username = os.Getenv("USERNAME")
	}
	snap.UserDomain = os.Getenv("USERDOMAIN")
	snap.IsElevatedAdmin = isProcessElevated()

	// RAM libre actual
	var mem memoryStatusEx
	mem.cbSize = uint32(unsafe.Sizeof(mem))
	r1, _, _ := procGlobalMemoryStatus.Call(uintptr(unsafe.Pointer(&mem)))
	if r1 != 0 {
		snap.FreeRAMBytes = int64(mem.ullAvailPhys)
	}

	// Uptime del sistema
	ms, _, _ := procGetTickCount64.Call()
	snap.SystemUptimeSeconds = int64(ms / 1000)

	// Espacio en disco destino
	if backupDir == "" {
		backupDir = "C:\\"
	}
	drive := filepath.VolumeName(backupDir)
	if drive == "" {
		drive = "C:"
	}
	snap.BackupDiskDrive = drive

	var freeBytes, totalBytes, totalFreeBytes uint64
	ptrPath, err := windows.UTF16PtrFromString(drive + "\\")
	if err == nil {
		if err := windows.GetDiskFreeSpaceEx(ptrPath, &freeBytes, &totalBytes, &totalFreeBytes); err == nil {
			snap.BackupDiskFreeBytes = int64(freeBytes)
			snap.BackupDiskTotalBytes = int64(totalBytes)
		}
	}

	return snap
}

func isProcessElevated() bool {
	var token windows.Token
	err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token)
	if err != nil {
		return false
	}
	defer token.Close()
	return token.IsElevated()
}
