module github.com/nickgarlis/rdma

go 1.27.0

require (
	github.com/mdlayher/netlink v1.11.2
	github.com/mdlayher/socket v0.8.0
	golang.org/x/sys v0.49.0
)

require (
	github.com/google/go-cmp v0.7.0 // indirect
	golang.org/x/net v0.61.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
)

replace golang.org/x/sys => ../sys
