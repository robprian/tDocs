package web

import (
	"fmt"
	"net"
)

// FindAvailableListener tries binding to startPort, sequentially probing subsequent
// ports if the requested port is already in use.
func FindAvailableListener(host string, startPort, maxProbes int) (net.Listener, int, error) {
	if maxProbes <= 0 {
		maxProbes = 50
	}

	for offset := 0; offset < maxProbes; offset++ {
		currentPort := startPort + offset
		addr := fmt.Sprintf("%s:%d", host, currentPort)
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			return ln, currentPort, nil
		}
	}

	return nil, 0, fmt.Errorf("could not find an available port in range %d-%d", startPort, startPort+maxProbes-1)
}

// GetLocalIPs returns a list of non-loopback IPv4 addresses assigned to the machine's interfaces.
func GetLocalIPs() []string {
	var ips []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}

	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipv4 := ipNet.IP.To4(); ipv4 != nil {
				ips = append(ips, ipv4.String())
			}
		}
	}
	return ips
}
