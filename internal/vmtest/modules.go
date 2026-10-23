package main

import (
	"bufio"
	"bytes"
	"debug/elf"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// A moduleTree is a kernel module directory, such as /lib/modules/<release>.
type moduleTree struct {
	// paths maps module names to their files.
	paths map[string]string

	// builtin holds the modules compiled into the kernel.
	builtin map[string]bool
}

// openModuleTree indexes the modules under dir. It does not need
// modules.dep, which distribution packages leave to depmod at install time.
func openModuleTree(dir string) (*moduleTree, error) {
	t := &moduleTree{
		paths:   make(map[string]string),
		builtin: make(map[string]bool),
	}

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() && strings.Contains(d.Name(), ".ko") {
			t.paths[moduleName(path)] = path
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("index modules: %w", err)
	}

	f, err := os.Open(filepath.Join(dir, "modules.builtin"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	if f != nil {
		defer f.Close()

		s := bufio.NewScanner(f)
		for s.Scan() {
			t.builtin[moduleName(s.Text())] = true
		}

		if err := s.Err(); err != nil {
			return nil, err
		}
	}

	return t, nil
}

// available returns the names that are modules in the tree.
func (t *moduleTree) available(names ...string) []string {
	var out []string
	for _, n := range names {
		if _, ok := t.paths[moduleName(n)]; ok {
			out = append(out, n)
		}
	}

	return out
}

// A module is a kernel module ready to be loaded.
type module struct {
	name string
	data []byte
}

// resolve returns the modules needed to load names, decompressed and in load
// order: every module comes after the modules it depends on. Modules built
// into the kernel are skipped.
func (t *moduleTree) resolve(names ...string) ([]module, error) {
	var (
		order []module
		seen  = make(map[string]bool)
	)

	var visit func(name string) error
	visit = func(name string) error {
		if seen[name] || t.builtin[name] {
			return nil
		}

		seen[name] = true

		path, ok := t.paths[name]
		if !ok {
			return fmt.Errorf("kernel module %s not found", name)
		}

		data, err := readModule(path)
		if err != nil {
			return err
		}

		deps, err := moduleDepends(data)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}

		for _, d := range deps {
			if err := visit(d); err != nil {
				return err
			}
		}

		order = append(order, module{name: name, data: data})
		return nil
	}

	for _, n := range names {
		if err := visit(moduleName(n)); err != nil {
			return nil, err
		}
	}

	return order, nil
}

// moduleName returns the canonical name of a module file or name, such as
// "rdma_rxe" for ".../rdma_rxe.ko.zst".
func moduleName(path string) string {
	base, _, _ := strings.Cut(filepath.Base(path), ".ko")
	return strings.ReplaceAll(base, "-", "_")
}

// readModule reads a module file, decompressing it if needed.
func readModule(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	switch {
	case strings.HasSuffix(path, ".ko"):
		return b, nil
	case strings.HasSuffix(path, ".ko.zst"):
		d, err := zstd.NewReader(nil)
		if err != nil {
			return nil, err
		}
		defer d.Close()

		out, err := d.DecodeAll(b, nil)
		if err != nil {
			return nil, fmt.Errorf("decompress %s: %w", path, err)
		}

		return out, nil
	default:
		return nil, fmt.Errorf("%s: unsupported module compression", path)
	}
}

// moduleDepends returns the modules a module depends on, from the depends=
// entry of its .modinfo section.
func moduleDepends(data []byte) ([]string, error) {
	f, err := elf.NewFile(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	sec := f.Section(".modinfo")
	if sec == nil {
		return nil, fmt.Errorf("no .modinfo section")
	}

	info, err := sec.Data()
	if err != nil {
		return nil, err
	}

	for entry := range bytes.SplitSeq(info, []byte{0}) {
		deps, ok := bytes.CutPrefix(entry, []byte("depends="))
		if !ok {
			continue
		}

		var names []string
		for d := range strings.SplitSeq(string(deps), ",") {
			if d != "" {
				names = append(names, moduleName(d))
			}
		}

		return names, nil
	}

	return nil, nil
}
