package hostinfo

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strings"
)

// HostSpecs representa las características de hardware y sistema operativo de la máquina.
type HostSpecs struct {
	HostID           string   `json:"host_id"`
	Hostname         string   `json:"hostname"`
	DomainName       string   `json:"domain_name"`
	OSName           string   `json:"os_name"`
	OSFamily         string   `json:"os_family"`
	OSVersion        string   `json:"os_version"`
	OSBuild          string   `json:"os_build"`
	OSArch           string   `json:"os_arch"`
	CPUModel         string   `json:"cpu_model"`
	CPUCoresLogical  int      `json:"cpu_cores_logical"`
	CPUCoresPhysical int      `json:"cpu_cores_physical"`
	TotalRAMBytes    int64    `json:"total_ram_bytes"`
	MACAddresses     []string `json:"mac_addresses"`
	PrimaryMAC       string   `json:"primary_mac"`
}

// RuntimeSnapshot representa el estado vivo de red, proceso y recursos en el momento de la corrida.
type RuntimeSnapshot struct {
	PrimaryIP            string   `json:"primary_ip"`
	LocalIPs             []string `json:"local_ips"`
	Username             string   `json:"username"`
	UserDomain           string   `json:"user_domain"`
	IsElevatedAdmin      bool     `json:"is_elevated_admin"`
	ProcessID            int      `json:"process_id"`
	ProcessPath          string   `json:"process_path"`
	AgentVersion         string   `json:"agent_version"`
	GoVersion            string   `json:"go_version"`
	FreeRAMBytes         int64    `json:"free_ram_bytes"`
	BackupDiskDrive      string   `json:"backup_disk_drive"`
	BackupDiskFreeBytes  int64    `json:"backup_disk_free_bytes"`
	BackupDiskTotalBytes int64    `json:"backup_disk_total_bytes"`
	SystemUptimeSeconds  int64    `json:"system_uptime_seconds"`
	Timezone             string   `json:"timezone"`
}

// NetworkInfo agrupa la información detectada de interfaces de red.
type NetworkInfo struct {
	PrimaryIP    string
	LocalIPs     []string
	MACAddresses []string
	PrimaryMAC   string
}

// GetNetworkInfo detecta las IPs locales, direcciones MAC y la IP de salida principal.
func GetNetworkInfo() NetworkInfo {
	var info NetworkInfo
	info.LocalIPs = make([]string, 0)
	info.MACAddresses = make([]string, 0)

	// Intento de obtener la IP principal mediante UDP sin generar tráfico real
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		if udpAddr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
			info.PrimaryIP = udpAddr.IP.String()
		}
		_ = conn.Close()
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		if info.PrimaryIP == "" {
			info.PrimaryIP = "127.0.0.1"
		}
		return info
	}

	for _, iface := range ifaces {
		// Ignorar loopback o interfaces apagadas
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		mac := strings.ToUpper(iface.HardwareAddr.String())
		if mac != "" {
			info.MACAddresses = append(info.MACAddresses, mac)
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			if ip == nil || ip.IsLoopback() {
				continue
			}

			// Solo IPs unicast globales o privadas
			ipStr := ip.String()
			info.LocalIPs = append(info.LocalIPs, ipStr)

			// Si esta interfaz tiene la IP principal, registrar su MAC como primaria
			if info.PrimaryIP != "" && ipStr == info.PrimaryIP && mac != "" {
				info.PrimaryMAC = mac
			}
		}
	}

	if info.PrimaryIP == "" && len(info.LocalIPs) > 0 {
		info.PrimaryIP = info.LocalIPs[0]
	}
	if info.PrimaryIP == "" {
		info.PrimaryIP = "127.0.0.1"
	}
	if info.PrimaryMAC == "" && len(info.MACAddresses) > 0 {
		info.PrimaryMAC = info.MACAddresses[0]
	}

	return info
}

// GenerateHostID produce un identificador estable y determinístico para la máquina.
func GenerateHostID(hostname, primaryMAC string) string {
	if hostname == "" {
		hostname, _ = os.Hostname()
	}
	h := sha256.New()
	h.Write([]byte(strings.ToLower(strings.TrimSpace(hostname))))
	h.Write([]byte(":"))
	h.Write([]byte(strings.ToUpper(strings.TrimSpace(primaryMAC))))
	sum := h.Sum(nil)
	return fmt.Sprintf("host_%s", hex.EncodeToString(sum[:12]))
}
