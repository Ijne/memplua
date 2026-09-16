//go:build windows

package inference

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var getExtendedTCPTable = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")

const tcpTableOwnerPIDListener = 3

type processGuard struct{ job windows.Handle }

func configureProcess(command *exec.Cmd) (*processGuard, error) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create llama job object: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return nil, fmt.Errorf("configure llama job object: %w", err)
	}
	return &processGuard{job: job}, nil
}

func (g *processGuard) attach(process *os.Process) error {
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(process.Pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	return windows.AssignProcessToJobObject(g.job, handle)
}

func (g *processGuard) close() error { return windows.CloseHandle(g.job) }

func terminateOwnedProcess(pid int, expectedPath string) (bool, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		if err == windows.ERROR_INVALID_PARAMETER {
			return false, nil
		}
		return false, err
	}
	defer windows.CloseHandle(handle)
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	if err = windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size); err != nil {
		return false, err
	}
	actual := filepath.Clean(windows.UTF16ToString(buffer[:size]))
	expected, err := filepath.Abs(expectedPath)
	if err != nil {
		return false, err
	}
	if !strings.EqualFold(actual, filepath.Clean(expected)) {
		return false, nil
	}
	if err = windows.TerminateProcess(handle, 1); err != nil {
		return false, err
	}
	return true, nil
}

func terminateOwnedProcessOnPort(port int, expectedPath string) (bool, error) {
	if port <= 0 || port > 65535 {
		return false, nil
	}
	var size uint32
	result, _, _ := getExtendedTCPTable.Call(0, uintptr(unsafe.Pointer(&size)), 0, windows.AF_INET, tcpTableOwnerPIDListener, 0)
	if result != uintptr(windows.ERROR_INSUFFICIENT_BUFFER) || size < 4 {
		if result == 0 {
			return false, nil
		}
		return false, fmt.Errorf("size TCP owner table: windows error %d", result)
	}
	buffer := make([]byte, size)
	result, _, _ = getExtendedTCPTable.Call(uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)), 0, windows.AF_INET, tcpTableOwnerPIDListener, 0)
	if result != 0 {
		return false, fmt.Errorf("read TCP owner table: windows error %d", result)
	}
	count := int(binary.LittleEndian.Uint32(buffer[:4]))
	const rowSize = 24
	for index := 0; index < count; index++ {
		offset := 4 + index*rowSize
		if offset+rowSize > len(buffer) {
			break
		}
		localPort := binary.LittleEndian.Uint32(buffer[offset+8 : offset+12])
		decodedPort := int((localPort&0xff)<<8 | (localPort&0xff00)>>8)
		if decodedPort != port {
			continue
		}
		pid := int(binary.LittleEndian.Uint32(buffer[offset+20 : offset+24]))
		return terminateOwnedProcess(pid, expectedPath)
	}
	return false, nil
}
