//go:build linux

package rdma

import (
	"errors"
	"fmt"
)

// A CQ is a completion queue.
type CQ struct {
	// Device is the Device the CQ was created on.
	Device *Device

	// Handle is the kernel handle of the CQ.
	Handle uint32

	// CQE is the number of completions the CQ holds, which may be more than
	// was asked for.
	CQE uint32

	// Channel is the completion channel the CQ reports to, if any.
	Channel *CompChannel

	userHandle uint64

	providerData
}

// CreateCQ creates a completion queue for at least cqe completions, like
// ibv_create_cq. channel, if not nil, receives a notification when a
// completion is added after Device.ReqNotifyCQ; compVector selects the completion
// vector used for notifications, below Device.NumCompVectors.
func (d *Device) CreateCQ(cqe uint32, channel *CompChannel, compVector uint32) (*CQ, error) {
	if channel != nil {
		if err := d.owns(channel.Device); err != nil {
			return nil, fmt.Errorf("rdma: create CQ: %w", err)
		}
	}

	cq, err := d.provider.CreateCQ(cqe, channel, compVector)
	if err != nil {
		return nil, err
	}

	d.registerCQ(cq)
	return cq, nil
}

// PollCQ fills wc with up to len(wc) completions from cq and returns how many
// it filled, like ibv_poll_cq. It does not block.
func (d *Device) PollCQ(cq *CQ, wc []WC) (int, error) {
	if err := d.owns(cq.Device); err != nil {
		return 0, err
	}

	return d.provider.PollCQ(cq, wc)
}

// ReqNotifyCQ requests a notification on cq's completion channel for the next
// completion, or the next solicited one if solicitedOnly is set, like
// ibv_req_notify_cq.
func (d *Device) ReqNotifyCQ(cq *CQ, solicitedOnly bool) error {
	if err := d.owns(cq.Device); err != nil {
		return err
	}

	if cq.Channel == nil {
		return errors.New("rdma: CQ has no completion channel")
	}

	return d.provider.ReqNotifyCQ(cq, solicitedOnly)
}

// DestroyCQ destroys cq. QPs using it must be destroyed first. cq must not
// be used afterwards: the kernel reuses its handle for new objects.
func (d *Device) DestroyCQ(cq *CQ) error {
	if err := d.owns(cq.Device); err != nil {
		return err
	}

	if err := d.provider.DestroyCQ(cq); err != nil {
		return err
	}

	d.unregisterCQ(cq.userHandle)
	return nil
}
