#!/usr/bin/env bash
# Runs the integration tests (internal/integration) in a qemu VM.
#
#	scripts/integration.sh [go test flags, such as -test.run=TestSendRecv]
set -euo pipefail

# Linux 7.3-rc7 with ib_uverbs, rdma_rxe and dummy built in, from this
# repository's releases. The release notes say how it was built.
url=https://github.com/nickgarlis/rdma/releases/download/kernel-7.3-rc7-rxe/vmlinuz
kernel=$PWD/.cache/kernel-7.3-rc7-rxe/vmlinuz

if [ ! -f "$kernel" ]; then
	mkdir -p "$(dirname "$kernel")"
	curl -fsSL -o "$kernel.download" "$url"
	mv "$kernel.download" "$kernel"
fi

out=$PWD/.cache/integration
rm -rf "$out" && mkdir -p "$out"

cd internal/integration
CGO_ENABLED=0 go build -o "$out/setup" ./setup
CGO_ENABLED=0 go test -c -o "$out/integration.test" .

# The VM's init runs setup, which creates the rxe device, then the tests.
go tool bluebox -o "$out/initramfs.cpio" \
	-v RDMA_TEST_DEVICE=rxe0 -v RDMA_TEST_NETDEV=vmtest0 \
	-e "$out/setup" -e "$out/integration.test:\"-test.v $*\""

# The console is printed as it comes, and the script passes only if both
# setup and the tests exited with status 0. qemu's stdin is /dev/null so that
# it leaves the terminal alone and Ctrl-C stops it.
exec 3>&1
passed=$(qemu-system-x86_64 -enable-kvm -cpu host -smp 2 -m 1G \
	-nographic -no-reboot -nodefaults -serial stdio \
	-kernel "$kernel" -initrd "$out/initramfs.cpio" \
	-append "console=ttyS0 panic=-1 quiet" </dev/null |
	tee >(cat >&3) | grep -c 'exited, exit status 0' || true)
[ "$passed" -eq 2 ]
