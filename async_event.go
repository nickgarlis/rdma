//go:build linux

package rdma

import (
	"context"
	"encoding/binary"
	"fmt"

	"golang.org/x/sys/unix"
)

// An EventType is the type of an asynchronous event.
type EventType uint32

// Asynchronous event types, from enum ib_event_type.
const (
	EventCQErr EventType = iota
	EventQPFatal
	EventQPReqErr
	EventQPAccessErr
	EventCommEst
	EventSQDrained
	EventPathMig
	EventPathMigErr
	EventDeviceFatal
	EventPortActive
	EventPortErr
	EventLIDChange
	EventPKeyChange
	EventSMChange
	EventSRQErr
	EventSRQLimitReached
	EventQPLastWQEReached
	EventClientReregister
	EventGIDChange
	EventWQFatal
)

var eventTypeNames = [...]string{
	"CQ error",
	"QP fatal",
	"QP request error",
	"QP access error",
	"communication established",
	"SQ drained",
	"path migrated",
	"path migration error",
	"device fatal",
	"port active",
	"port error",
	"LID change",
	"P_Key change",
	"SM change",
	"SRQ error",
	"SRQ limit reached",
	"last WQE reached",
	"client reregister",
	"GID change",
	"WQ fatal",
}

func (t EventType) String() string {
	if int(t) < len(eventTypeNames) {
		return eventTypeNames[t]
	}

	return fmt.Sprintf("EventType(%d)", uint32(t))
}

// An AsyncEvent is an asynchronous event reported by the device, such as a
// QP error or a port state change.
type AsyncEvent struct {
	EventType EventType

	// The object the event refers to. Only the field matching EventType is
	// set, and it is nil if the object was already destroyed.
	CQ   *CQ
	QP   *QP
	Port uint32
}

// allocAsyncEventFile allocates the asynchronous event file right after the
// kernel context, as rdma-core does: the first one becomes the default for
// objects created without an explicit event file, and objects created
// before it exists would have nowhere to report their events.
func (d *Device) allocAsyncEventFile() error {
	fd, err := d.Cmd().allocAsyncFD()
	if err != nil {
		return err
	}

	async, err := newEventFile(fd, "uverbs-async")
	if err != nil {
		return err
	}

	d.async = async
	return nil
}

// GetAsyncEvent blocks until the device reports an asynchronous event or
// ctx is done.
func (d *Device) GetAsyncEvent(ctx context.Context) (AsyncEvent, error) {
	var (
		desc unix.IBUverbsAsyncEventDesc
		b    = make([]byte, binary.Size(desc))
	)

	n, err := d.async.ReadContext(ctx, b)
	if err != nil {
		return AsyncEvent{}, err
	}

	if _, err := binary.Decode(b[:n], binary.NativeEndian, &desc); err != nil {
		return AsyncEvent{}, fmt.Errorf("rdma: short async event read of %d bytes", n)
	}

	element := desc.Element
	ev := AsyncEvent{EventType: EventType(desc.Event_type)}

	switch ev.EventType {
	case EventCQErr:
		ev.CQ = d.lookupCQ(element)
	case EventPortActive, EventPortErr, EventLIDChange, EventPKeyChange,
		EventSMChange, EventClientReregister, EventGIDChange, EventDeviceFatal:
		ev.Port = uint32(element)
	default:
		ev.QP = d.lookupQP(element)
	}

	return ev, nil
}
