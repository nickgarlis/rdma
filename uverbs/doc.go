//go:build linux

// Package uverbs implements the kernel's user-space verbs interface
// (ib_uverbs): the RDMA_VERBS_IOCTL ioctl on /dev/infiniband/uverbsN.
//
// Most programs use package rdma, which builds on uverbs. Use uverbs to call
// kernel methods that rdma does not wrap, or to add driver-specific
// attributes when writing a provider.
package uverbs
