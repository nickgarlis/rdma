// Command vmtest runs integration tests inside a throwaway qemu/KVM virtual
// machine, so they can run as root and load kernel modules without touching
// the host.
//
// vmtest builds the setup command in ./setup and a static test binary for
// each package directory. It packs them and the kernel modules into an
// initramfs with bluebox (github.com/florianl/bluebox), boots the host's
// kernel with it and reports whether every executable exited successfully.
//
// bluebox's init mounts the usual file systems and runs the executables in
// order: setup first, which loads the modules and creates an rxe device,
// then the test binaries, which find the device in RDMA_TEST_DEVICE and its
// interface in RDMA_TEST_NETDEV.
//
//	cd internal/vmtest && go run . ../../providers/rxe
//
// By default it boots a pinned Ubuntu mainline build of the kernel, which is
// downloaded once and cached, so every machine tests the same kernel. -host
// boots the running kernel instead, and -kernel with -modules any other.
//
// It needs qemu-system-x86_64 and access to /dev/kvm.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"github.com/florianl/bluebox/initramfs"
	"golang.org/x/sys/unix"
)

// modules are loaded in the guest, along with their dependencies.
var modules = []string{"ib_uverbs", "rdma_rxe", "dummy"}

// optionalModules are loaded in the guest if the kernel has them. They are
// requested at run time rather than listed as dependencies, which needs
// modprobe: older kernels, such as 6.12, give rxe its packet checksum through
// the "crc32" crypto algorithm.
var optionalModules = []string{"crc32_generic"}

// defaultUbuntu is the Ubuntu mainline kernel version booted by default.
const defaultUbuntu = "7.2.6"

func main() {
	log.SetFlags(0)
	log.SetPrefix("vmtest: ")

	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	var (
		ubuntu  = flag.String("ubuntu", defaultUbuntu, "boot this Ubuntu mainline kernel version, from "+mainlineURL)
		host    = flag.Bool("host", false, "boot the running kernel instead")
		kernel  = flag.String("kernel", "", "boot this kernel image instead")
		modDir  = flag.String("modules", "", "kernel module directory matching -kernel; omit it if everything is built in")
		timeout = flag.Duration("timeout", 5*time.Minute, "time limit for the VM")
		runRE   = flag.String("run", "", "only run tests matching this regular expression")
		kdebug  = flag.String("kdebug", "", "enable kernel dynamic debug for this module in the guest, such as rdma_rxe")
		keep    = flag.Bool("keep", false, "keep the work directory")
	)

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: vmtest [flags] package-dir...\n")
		flag.PrintDefaults()
	}

	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}

	work, err := os.MkdirTemp("", "vmtest")
	if err != nil {
		return err
	}

	if *keep {
		log.Printf("work directory: %s", work)
	} else {
		defer os.RemoveAll(work)
	}

	b := initramfs.New()
	b.Setenv("RDMA_TEST_DEVICE", "rxe0")
	b.Setenv("RDMA_TEST_NETDEV", "vmtest0")
	if *kdebug != "" {
		b.Setenv("RDMA_VM_KDEBUG", *kdebug)
	}

	args := []string{"-test.v", "-test.count=1"}
	if *runRE != "" {
		args = append(args, "-test.run="+*runRE)
	}

	setup := filepath.Join(work, "setup")
	if err := buildSetup(setup); err != nil {
		return err
	}

	// bluebox runs the executables in the order they are added.
	if err := b.Execute(setup); err != nil {
		return err
	}

	names := []string{"setup"}
	for _, dir := range flag.Args() {
		bin := filepath.Join(work, filepath.Base(dir)+".test")
		if err := buildTest(dir, bin); err != nil {
			return err
		}

		if err := b.Execute(bin, args...); err != nil {
			return err
		}

		names = append(names, filepath.Base(bin))
	}

	kernelPath, modulePath, err := selectKernel(*ubuntu, *host, *kernel, *modDir)
	if err != nil {
		return err
	}

	// A kernel without a module directory has everything built in.
	var mods []module
	if modulePath != "" {
		tree, err := openModuleTree(modulePath)
		if err != nil {
			return err
		}

		mods, err = tree.resolve(append(tree.available(optionalModules...), modules...)...)
		if err != nil {
			return err
		}
	}

	// The guest loads modules in lexical order, so number them in load
	// order.
	for i, m := range mods {
		dst := filepath.Join(work, fmt.Sprintf("%02d-%s.ko", i, m.name))
		if err := os.WriteFile(dst, m.data, 0o644); err != nil {
			return err
		}

		if err := b.Embed(dst); err != nil {
			return err
		}
	}

	initrd := filepath.Join(work, "initramfs.cpio")
	f, err := os.Create(initrd)
	if err != nil {
		return err
	}

	if err := b.Generate(f); err != nil {
		f.Close()
		return fmt.Errorf("generate initramfs: %w", err)
	}

	if err := f.Close(); err != nil {
		return err
	}

	log.Printf("booting %s", kernelPath)
	return boot(kernelPath, initrd, *timeout, names)
}

// selectKernel returns the kernel image and module directory to boot.
func selectKernel(ubuntu string, host bool, kernel, modules string) (string, string, error) {
	switch {
	case kernel != "" || modules != "":
		if kernel == "" {
			return "", "", errors.New("-modules needs -kernel")
		}

		return kernel, modules, nil
	case host:
		release := kernelRelease()
		for _, k := range []string{
			filepath.Join("/usr/lib/modules", release, "vmlinuz"),
			filepath.Join("/boot", "vmlinuz-"+release),
		} {
			if _, err := os.Stat(k); err == nil {
				return k, filepath.Join("/lib/modules", release), nil
			}
		}

		return "", "", fmt.Errorf("no kernel image found for the running kernel %s", release)
	default:
		cache, err := os.UserCacheDir()
		if err != nil {
			return "", "", err
		}

		return ubuntuKernel(ubuntu, filepath.Join(cache, "rdma-vmtest"))
	}
}

func kernelRelease() string {
	var u unix.Utsname
	if err := unix.Uname(&u); err != nil {
		return ""
	}

	return unix.ByteSliceToString(u.Release[:])
}

// buildSetup builds the setup command of this module as a static binary.
func buildSetup(out string) error {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return fmt.Errorf("no build information; run vmtest with go run or go build")
	}

	cmd := exec.Command("go", "build", "-o", out, bi.Path+"/setup")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build setup: %w", err)
	}

	return nil
}

// buildTest builds the tests of the package in dir as a static binary.
func buildTest(dir, out string) error {
	cmd := exec.Command("go", "test", "-c", "-o", out, ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build tests in %s: %w", dir, err)
	}

	return nil
}

// exitRE matches the line bluebox's init prints when an executable exits.
var exitRE = regexp.MustCompile(`\./(\S+) exited, exit status (-?\d+)`)

// boot runs the VM and checks that each of the named executables exited
// with status 0.
func boot(kernel, initrd string, timeout time.Duration, names []string) error {
	cmd := exec.Command("qemu-system-x86_64",
		"-enable-kvm", "-cpu", "host", "-smp", "2", "-m", "1G",
		"-nographic", "-no-reboot", "-nodefaults", "-serial", "stdio",
		"-kernel", kernel, "-initrd", initrd,
		"-append", "console=ttyS0 panic=-1 quiet loglevel=4",
	)

	var out bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &out)
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	timer := time.AfterFunc(timeout, func() { _ = cmd.Process.Kill() })
	defer timer.Stop()

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("qemu: %w", err)
	}

	status := make(map[string]string)
	for _, m := range exitRE.FindAllStringSubmatch(out.String(), -1) {
		status[m[1]] = m[2]
	}

	// A failed setup makes every test fail, so report it on its own.
	if s, ok := status["setup"]; ok && s != "0" {
		return fmt.Errorf("setup failed with exit status %s; see its output above", s)
	}

	var failed []string
	for _, n := range names {
		switch s, ok := status[n]; {
		case !ok:
			failed = append(failed, n+" (did not finish)")
		case s != "0":
			failed = append(failed, n)
		}
	}

	if len(failed) > 0 {
		return fmt.Errorf("failed: %s", strings.Join(failed, ", "))
	}

	return nil
}
