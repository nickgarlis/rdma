//go:build linux

// Package integration holds the tests that need a real RDMA device. They run
// as root in a throwaway VM, which scripts/integration.sh boots after setup has
// created an rxe device; the device and its interface are named by
// RDMA_TEST_DEVICE and RDMA_TEST_NETDEV.
package integration

import (
	"os"
	"testing"
)

// testEnv returns the environment variable key, which the VM sets.
func testEnv(t *testing.T, key string) string {
	t.Helper()

	v := os.Getenv(key)
	if v == "" {
		t.Fatalf("%s is not set; run the tests with scripts/integration.sh", key)
	}

	return v
}
