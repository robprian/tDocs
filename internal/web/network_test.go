package web

import (
	"net"
	"testing"
)

func TestFindAvailableListener_AutoScan(t *testing.T) {
	// 1. Bind an ephemeral port to occupy it
	occupiedListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to bind occupied listener: %v", err)
	}
	defer occupiedListener.Close()

	occupiedPort := occupiedListener.Addr().(*net.TCPAddr).Port

	// 2. Request FindAvailableListener starting at occupiedPort
	ln, chosenPort, err := FindAvailableListener("127.0.0.1", occupiedPort, 5)
	if err != nil {
		t.Fatalf("FindAvailableListener failed: %v", err)
	}
	defer ln.Close()

	// 3. Verify it auto-scanned past occupiedPort
	if chosenPort == occupiedPort {
		t.Fatalf("FindAvailableListener bound to already occupied port %d!", occupiedPort)
	}
	if chosenPort != occupiedPort+1 {
		t.Logf("Note: bound to port %d (next available after %d)", chosenPort, occupiedPort)
	}
}

func TestGetLocalIPs(t *testing.T) {
	ips := GetLocalIPs()
	// Just verify function executes cleanly and doesn't return loopbacks
	for _, ip := range ips {
		if ip == "127.0.0.1" || ip == "localhost" {
			t.Fatalf("GetLocalIPs returned loopback address: %s", ip)
		}
	}
}
