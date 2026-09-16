//go:build windows

package inference

import (
	"fmt"
	"net"
	"os/exec"
	"testing"
	"time"
)

func powershellForProcessTest(t *testing.T, script string) (*exec.Cmd, string) {
	t.Helper()
	path, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Skip("powershell.exe is unavailable")
	}
	return exec.Command(path, "-NoProfile", "-NonInteractive", "-Command", script), path
}

func TestProcessGuardKillsManagedProcessWhenClosed(t *testing.T) {
	command, _ := powershellForProcessTest(t, "Start-Sleep -Seconds 30")
	guard, err := configureProcess(command)
	if err != nil {
		t.Fatal(err)
	}
	if err = command.Start(); err != nil {
		guard.close()
		t.Fatal(err)
	}
	if err = guard.attach(command.Process); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		guard.close()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	if err = guard.close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = command.Process.Kill()
		t.Fatal("closing the job object did not terminate its process")
	}
}

func TestTerminateOwnedProcessOnListeningPort(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	command, path := powershellForProcessTest(t, fmt.Sprintf("$listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, %d); $listener.Start(); Start-Sleep -Seconds 30", port))
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		killed, killErr := terminateOwnedProcessOnPort(port, path)
		if killErr != nil {
			_ = command.Process.Kill()
			t.Fatal(killErr)
		}
		if killed {
			select {
			case <-done:
				return
			case <-time.After(5 * time.Second):
				t.Fatal("terminated port owner did not exit")
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = command.Process.Kill()
	<-done
	t.Fatal("managed process was not found on its listening port")
}
