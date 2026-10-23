//go:build linux

// Package rdma implements the Linux RDMA verbs (ibverbs) interface in pure Go,
// following rdma-core's libibverbs: Open is ibv_open_device, and every verb is
// a method of the opened Device that takes the objects it acts on, such as
// Device.CreateQP for ibv_create_qp or Device.PostSend for ibv_post_send.
//
// # Writing a provider
//
// The data path differs per kind of device, so it lives in a provider, like
// an rdma-core provider library. A provider implements the Provider
// interface for one Device: every verb that depends on the device, like
// struct verbs_context_ops, plus Init, which creates the kernel context with
// Cmd.GetContext like an rdma-core provider's alloc_context. It runs the
// generic kernel commands with the Device's Cmd (Cmd.CreateCQ, Cmd.AllocPD,
// …), like the ibv_cmd_* functions of <infiniband/driver.h>, passing its
// driver data where it needs it, and attaches its own state to objects with
// SetProviderData.
//
// A provider package offers an Open function, such as rxe.Open, that calls
// Open with a new Provider. The Device's verbs check that the objects passed
// to them belong to the Device, then call its Provider. Applications only use
// the Device's verbs.
package rdma
