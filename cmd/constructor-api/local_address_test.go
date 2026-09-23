package main

import "testing"

func TestValidateLoopbackListenAddress(t *testing.T) {
	for _, address := range []string{"127.0.0.1:8787", "[::1]:8787"} {
		if err := validateLoopbackListenAddress(address); err != nil {
			t.Errorf("expected %q to be allowed: %v", address, err)
		}
	}
	for _, address := range []string{"0.0.0.0:8787", "192.168.1.10:8787", ":8787", "localhost:8787", "127.0.0.1:0", "127.0.0.1:65536"} {
		if err := validateLoopbackListenAddress(address); err == nil {
			t.Errorf("expected %q to be rejected", address)
		}
	}
}
