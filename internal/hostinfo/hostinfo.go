// Package hostinfo collects a best-effort, static inventory at server startup.
// Missing platform tools never prevent the execution environment from starting.
package hostinfo

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/tk/t3go/internal/environment"
	"github.com/tk/t3go/internal/proto"
)

func command(ctx context.Context, program string, args ...string) string {
	out, err := exec.CommandContext(ctx, program, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func firstName(names ...string) string {
	for _, name := range names {
		if environment.UsableName(name) {
			return strings.TrimSpace(name)
		}
	}
	return ""
}

// Collect returns only static machine information, with no credentials.
func Collect() (string, *proto.SystemInfo) {
	hostname, _ := os.Hostname()
	info := &proto.SystemInfo{OS: runtime.GOOS, Arch: runtime.GOARCH, LogicalCPUs: runtime.NumCPU()}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	switch runtime.GOOS {
	case "darwin":
		info.OS = "macOS"
		if !environment.UsableName(hostname) {
			hostname = firstName(command(ctx, "/usr/sbin/scutil", "--get", "LocalHostName"),
				command(ctx, "/usr/sbin/scutil", "--get", "ComputerName"))
		}
		info.OSVersion = command(ctx, "/usr/bin/sw_vers", "-productVersion")
		info.CPUModel = command(ctx, "/usr/sbin/sysctl", "-n", "machdep.cpu.brand_string")
		info.MemoryTotalBytes, _ = strconv.ParseUint(command(ctx, "/usr/sbin/sysctl", "-n", "hw.memsize"), 10, 64)
	case "linux":
		info.OS = "Linux"
		if raw, err := os.ReadFile("/etc/os-release"); err == nil {
			release := parseRelease(string(raw))
			if release["NAME"] != "" {
				info.OS = release["NAME"]
			}
			info.OSVersion = release["VERSION_ID"]
		}
		if raw, err := os.ReadFile("/proc/cpuinfo"); err == nil {
			info.CPUModel = cpuModel(string(raw))
		}
		if raw, err := os.ReadFile("/proc/meminfo"); err == nil {
			info.MemoryTotalBytes = memoryTotal(string(raw))
		}
		if !environment.UsableName(hostname) {
			raw, _ := os.ReadFile("/etc/hostname")
			hostname = firstName(string(raw))
		}
	case "windows":
		info.OS = "Windows"
		// CIM is available on supported Windows versions; one bounded query
		// avoids separate PowerShell processes for the inventory fields.
		raw := command(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
			"$o=Get-CimInstance Win32_OperatingSystem; $c=Get-CimInstance Win32_Processor | Select-Object -First 1; "+
				"$m=Get-CimInstance Win32_ComputerSystem; "+
				"@{os=$o.Caption;osVersion=$o.Version;cpuModel=$c.Name;memoryTotalBytes=[uint64]$m.TotalPhysicalMemory} | ConvertTo-Json -Compress")
		var details proto.SystemInfo
		if json.Unmarshal([]byte(raw), &details) == nil {
			if details.OS != "" {
				info.OS = details.OS
			}
			info.OSVersion, info.CPUModel, info.MemoryTotalBytes = details.OSVersion, details.CPUModel, details.MemoryTotalBytes
		}
		hostname = firstName(hostname, os.Getenv("COMPUTERNAME"))
	}
	return firstName(hostname), info
}

func parseRelease(raw string) map[string]string {
	values := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.HasPrefix(strings.TrimSpace(key), "#") {
			continue
		}
		value = strings.TrimSpace(value)
		if unquoted, err := strconv.Unquote(value); err == nil {
			value = unquoted
		} else {
			value = strings.Trim(value, "'")
		}
		values[strings.TrimSpace(key)] = value
	}
	return values
}

func cpuModel(raw string) string {
	values := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok && values[strings.TrimSpace(key)] == "" {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	for _, key := range []string{"model name", "Hardware", "Processor", "cpu model"} {
		if value := values[key]; value != "" {
			return value
		}
	}
	return ""
}

func memoryTotal(raw string) uint64 {
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == "MemTotal:" && fields[2] == "kB" {
			value, err := strconv.ParseUint(fields[1], 10, 64)
			if err == nil && value <= ^uint64(0)/1024 {
				return value * 1024
			}
		}
	}
	return 0
}
