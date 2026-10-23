//go:build linux

package rdma

// A PD is a protection domain. Memory regions and queue pairs belong to a PD
// and can only be used together within it.
type PD struct {
	// Device is the Device the PD was allocated on.
	Device *Device

	// Handle is the kernel handle of the PD.
	Handle uint32

	providerData
}

// AllocPD allocates a protection domain.
func (d *Device) AllocPD() (*PD, error) {
	return d.provider.AllocPD()
}

// DeallocPD deallocates pd. Objects created in it must be destroyed first.
// pd must not be used afterwards: the kernel reuses its handle for new
// objects.
func (d *Device) DeallocPD(pd *PD) error {
	if err := d.owns(pd.Device); err != nil {
		return err
	}

	return d.provider.DeallocPD(pd)
}
