package main

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
)

func validateLoopbackListenAddress(value string) error {
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		return fmt.Errorf("expected a loopback IP and port: %w", err)
	}
	address, err := netip.ParseAddr(host)
	if err != nil || address.Zone() != "" || !address.IsLoopback() {
		return fmt.Errorf("host must be a loopback IP address")
	}
	parsedPort, err := strconv.Atoi(port)
	if err != nil || parsedPort < 1 || parsedPort > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	return nil
}
