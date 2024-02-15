package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"podmanbootc/pkg/config"
)

func InitOSCDirs() error {
	if err := os.MkdirAll(config.ConfigDir, os.ModePerm); err != nil {
		return err
	}
	if err := os.MkdirAll(config.CacheDir, os.ModePerm); err != nil {
		return err
	}

	if err := os.MkdirAll(config.RunDir, os.ModePerm); err != nil {
		return err
	}

	return nil
}

// VM files
const (
	runConfigFile  = "run.json"
	runPidFile     = "run.pid"
	installPidFile = "install.pid"
	configFile     = "vm.json"
	diskImage      = "disk.qcow2"

	BootcOciArchive         = "image-archive.tar"
	BootcOciDir             = "image-dir"
	BootcCiDataDir          = "cidata"
	BootcCiDataIso          = "cidata.iso"
	BootcCiDefaultTransport = "cdrom"
	BootcSshKeyFile         = "sshkey"
	BootcSshPortFile        = "sshport"
	BootcCfgFile            = "bc.cfg"
)

type BcVmConfig struct {
	SshPort     int    `json:"SshPort"`
	SshIdentity string `json:"SshPriKey"`
}

// VM Status
const (
	Installing string = "Installing"
	Running           = "Running"
	Stopped           = "Stopped"
)

type RunVmConfig struct {
	SshPort uint64 `json:"SshPort"`
	VncPort uint64 `json:"VncPort"`
}

type VmConfig struct {
	Name       string `json:"Name"`
	Vcpu       uint64 `json:"VCPU"`
	Mem        uint64 `json:"Mem"`
	DiskSize   uint64 `json:"DiskSize"`
	DiskImage  string `json:"DiskImage"`
	RunPidFile string `json:"RunPidFile"`
	SshPriKey  string `json:"SshPriKey"`
}

func NewVM(name string, vcpu, mem, diskSize uint64) VmConfig {
	vm := NewVMPartial(name)
	vm.Vcpu = vcpu
	vm.Mem = mem
	vm.DiskSize = diskSize
	return vm
}

func NewVMPartial(name string) VmConfig {
	return VmConfig{
		Name:       name,
		DiskImage:  filepath.Join(config.ConfigDir, name, diskImage),
		RunPidFile: filepath.Join(config.RunDir, name, runPidFile),
		SshPriKey:  filepath.Join(config.SshDir, name),
	}
}

func (vm VmConfig) ConfigDir() string {
	return filepath.Dir(vm.DiskImage)
}

func (vm VmConfig) RunDir() string {
	return filepath.Dir(vm.RunPidFile)
}

func (vm VmConfig) ConfigFile() string {
	return filepath.Join(vm.ConfigDir(), configFile)
}

func (vm VmConfig) RunConfigFile() string {
	return filepath.Join(vm.RunDir(), runConfigFile)
}

func (vm VmConfig) InstallPidFile() string {
	return filepath.Join(vm.RunDir(), installPidFile)
}

func (vm VmConfig) SshKeys() (string, string) {
	pubKey := vm.SshPriKey + ".pub"
	return pubKey, vm.SshPriKey
}

func (vm VmConfig) Status() string {
	installPidFile := vm.InstallPidFile()
	runPidfile := vm.RunPidFile

	if live, _ := isProcessAlive(installPidFile); live {
		return Installing
	}

	if live, _ := isProcessAlive(runPidfile); live {
		return Running
	}
	return Stopped
}
func fileExist(path string) (bool, error) {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	return true, nil
}

func isCreated(name string) (bool, error) {
	probeVm := NewVMPartial(name)
	return fileExist(probeVm.ConfigFile())
}

func LoadVmFromDisk(name string) (*VmConfig, error) {
	exist, err := isCreated(name)
	if err != nil {
		return nil, err
	}

	if !exist {
		return nil, fmt.Errorf("VM '%s' does not exists", name)
	}

	probeVm := NewVMPartial(name)
	fileContent, err := os.ReadFile(probeVm.ConfigFile())
	if err != nil {
		return nil, err
	}

	vm := new(VmConfig)
	if err := json.Unmarshal(fileContent, vm); err != nil {
		return nil, err
	}
	return vm, nil
}

func LoadRunningVmFromDisk(name string) (*RunVmConfig, error) {
	exist, err := isCreated(name)
	if err != nil {
		return nil, err
	}

	if !exist {
		return nil, fmt.Errorf("VM '%s' does not exists", name)
	}

	probeVm := NewVMPartial(name)
	if probeVm.Status() != Running {
		return nil, fmt.Errorf("VM %s' is not running, you need to start it first", name)
	}

	fileContent, err := os.ReadFile(probeVm.RunConfigFile())
	if err != nil {
		return nil, err
	}

	runningVm := new(RunVmConfig)
	if err := json.Unmarshal(fileContent, runningVm); err != nil {
		return nil, err
	}
	return runningVm, nil
}

func isProcessAlive(pidFile string) (bool, error) {
	pid, err := readPidFile(pidFile)
	if err != nil {
		return false, err
	}
	return isPidAlive(pid), nil
}

func readPidFile(pidFile string) (int, error) {
	if _, err := os.Stat(pidFile); err != nil {
		return -1, err
	}

	fileContent, err := os.ReadFile(pidFile)
	if err != nil {
		return -1, err
	}
	pidStr := string(bytes.Trim(fileContent, "\n"))
	pid, err := strconv.ParseInt(pidStr, 10, 64)
	if err != nil {
		return -1, err
	}
	return int(pid), nil
}

func isPidAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	err = process.Signal(syscall.Signal(0))
	return err == nil
}

func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	exists := false

	if err == nil {
		exists = true
	} else if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	return exists, err
}

func bootcImagePath(id string) (string, error) {
	files, err := os.ReadDir(config.CacheDir)
	if err != nil {
		return "", err
	}

	imageId := ""
	for _, f := range files {
		if f.IsDir() && strings.HasPrefix(f.Name(), id) {
			imageId = f.Name()
		}
	}

	if imageId == "" {
		return "", fmt.Errorf("local installation '%s' does not exists", id)
	}

	return filepath.Join(config.CacheDir, imageId), nil
}

func loadConfig(id string) (*BcVmConfig, error) {
	vmPath, err := bootcImagePath(id)
	if err != nil {
		return nil, err
	}

	cfgFile := filepath.Join(vmPath, BootcCfgFile)
	fileContent, err := os.ReadFile(cfgFile)
	if err != nil {
		return nil, err
	}

	cfg := new(BcVmConfig)
	if err := json.Unmarshal(fileContent, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
