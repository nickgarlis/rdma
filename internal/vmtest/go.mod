module github.com/nickgarlis/rdma/internal/vmtest

go 1.27.0

require (
	github.com/florianl/bluebox v0.0.3-0.20261024072452-0d092538e91a
	github.com/klauspost/compress v1.19.1
	github.com/mdlayher/netlink v1.11.2
	github.com/nickgarlis/rdma v0.0.0
	golang.org/x/sys v0.49.0
)

require (
	github.com/cavaliergopher/cpio v1.0.1 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/mdlayher/socket v0.8.0 // indirect
	golang.org/x/net v0.61.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
)

replace github.com/nickgarlis/rdma => ../..
