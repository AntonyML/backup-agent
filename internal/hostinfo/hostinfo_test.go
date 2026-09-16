package hostinfo

import (
	"testing"
)

func TestCollectHostSpecs(t *testing.T) {
	specs := CollectHostSpecs()

	if specs.HostID == "" {
		t.Errorf("HostID no debe estar vacío")
	}
	if specs.Hostname == "" {
		t.Errorf("Hostname no debe estar vacío")
	}
	if specs.OSName == "" {
		t.Errorf("OSName no debe estar vacío")
	}
	if specs.OSArch == "" {
		t.Errorf("OSArch no debe estar vacío")
	}
	if specs.CPUCoresLogical <= 0 {
		t.Errorf("CPUCoresLogical debe ser > 0, dio %d", specs.CPUCoresLogical)
	}
	if specs.TotalRAMBytes <= 0 {
		t.Errorf("TotalRAMBytes debe ser > 0, dio %d", specs.TotalRAMBytes)
	}
}

func TestCollectRuntimeSnapshot(t *testing.T) {
	snap := CollectRuntimeSnapshot("C:\\")

	if snap.PrimaryIP == "" {
		t.Errorf("PrimaryIP no debe estar vacía")
	}
	if snap.Username == "" {
		t.Errorf("Username no debe estar vacío")
	}
	if snap.ProcessID <= 0 {
		t.Errorf("ProcessID debe ser > 0, dio %d", snap.ProcessID)
	}
	if snap.ProcessPath == "" {
		t.Errorf("ProcessPath no debe estar vacío")
	}
	if snap.AgentVersion == "" {
		t.Errorf("AgentVersion no debe estar vacía")
	}
	if snap.BackupDiskDrive == "" {
		t.Errorf("BackupDiskDrive no debe estar vacío")
	}
	if snap.BackupDiskTotalBytes <= 0 {
		t.Errorf("BackupDiskTotalBytes debe ser > 0, dio %d", snap.BackupDiskTotalBytes)
	}
}
