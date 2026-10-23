//go:build linux

package rdma

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/mdlayher/socket"
)

// A CompChannel is a completion channel: a file descriptor that reports
// which CQs have new completions after they were armed with Device.ReqNotifyCQ.
type CompChannel struct {
	// Device is the Device the channel was created on.
	Device *Device

	// FD is the channel's file descriptor.
	FD int

	conn *socket.Conn
}

// CreateCompChannel creates a completion channel.
func (d *Device) CreateCompChannel() (*CompChannel, error) {
	fd, err := d.Cmd().createCompChannel()
	if err != nil {
		return nil, err
	}

	f, err := newEventFile(fd, "uverbs-comp")
	if err != nil {
		return nil, err
	}

	return &CompChannel{Device: d, conn: f, FD: fd}, nil
}

// GetCQEvent blocks until an armed CQ attached to ch has a new
// completion, or ctx is done. The CQ must be armed again to receive further
// notifications.
func (d *Device) GetCQEvent(ctx context.Context, ch *CompChannel) (*CQ, error) {
	if err := d.owns(ch.Device); err != nil {
		return nil, err
	}

	for {
		var b [8]byte
		n, err := ch.conn.ReadContext(ctx, b[:])
		if err != nil {
			return nil, err
		}

		if n != len(b) {
			return nil, fmt.Errorf("rdma: short completion event read of %d bytes", n)
		}

		// Skip events for CQs that were destroyed in the meantime.
		if cq := d.lookupCQ(binary.NativeEndian.Uint64(b[:])); cq != nil {
			return cq, nil
		}
	}
}

// DestroyCompChannel closes ch. CQs using it must be destroyed first.
func (d *Device) DestroyCompChannel(ch *CompChannel) error {
	if err := d.owns(ch.Device); err != nil {
		return err
	}

	return ch.conn.Close()
}
