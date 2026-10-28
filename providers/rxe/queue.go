//go:build linux

package rxe

import (
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/nickgarlis/rdma"
	"github.com/nickgarlis/rdma/uverbs"
)

// Layout of struct rxe_queue_buf from <rdma/rdma_user_rxe.h>. The producer
// and consumer indices sit on separate cache lines ahead of the elements.
const (
	queueLog2ElemSize = 0
	queueIndexMask    = 4
	queueProducer     = 128
	queueConsumer     = 256
	queueData         = 384
)

// A queue is a struct rxe_queue_buf ring shared with the kernel. Indices are
// read and written atomically; the kernel pairs them with acquire and
// release ordering.
//
// Every access to the ring holds the queue's lock, taken with lock, which
// fails once the queue is closed. Closing unmaps the ring under the same
// lock, so a ring is never used while or after it is unmapped.
type queue struct {
	mu       sync.Mutex
	mem      *uverbs.Mapping // nil once closed
	buf      []byte
	log2Elem uint32
	mask     uint32
}

func mapQueue(dev *rdma.Device, mi mmapInfo) (*queue, error) {
	if mi.Size < queueData {
		return nil, fmt.Errorf("rxe: queue of %d bytes is too small", mi.Size)
	}

	mem, err := dev.Conn().Mmap(int64(mi.Offset), int(mi.Size), uverbs.ProtRead|uverbs.ProtWrite)
	if err != nil {
		return nil, fmt.Errorf("rxe: map queue: %w", err)
	}

	buf := mem.Bytes()
	q := &queue{
		mem:      mem,
		buf:      buf,
		log2Elem: binary.NativeEndian.Uint32(buf[queueLog2ElemSize:]),
		mask:     binary.NativeEndian.Uint32(buf[queueIndexMask:]),
	}

	if end := queueData + (uint64(q.mask)+1)<<q.log2Elem; end > uint64(len(buf)) {
		_ = mem.Close()
		return nil, fmt.Errorf("rxe: queue of %d elements of %d bytes exceeds mapping of %d bytes",
			q.mask+1, q.elemSize(), len(buf))
	}

	return q, nil
}

// lock locks the queue for using its ring. It fails, leaving the queue
// unlocked, if the queue is closed.
func (q *queue) lock() error {
	q.mu.Lock()
	if q.mem == nil {
		q.mu.Unlock()
		return errQueueClosed
	}

	return nil
}

func (q *queue) unlock() { q.mu.Unlock() }

// close unmaps the ring. Closing a closed queue does nothing.
func (q *queue) close() error {
	q.mu.Lock()
	defer q.mu.Unlock()

	return q.closeLocked()
}

// closeLocked is close for a caller that holds the lock.
func (q *queue) closeLocked() error {
	if q.mem == nil {
		return nil
	}

	err := q.mem.Close()
	q.mem, q.buf = nil, nil
	return err
}

func (q *queue) elemSize() int { return 1 << q.log2Elem }

func (q *queue) index(off int) *uint32 {
	return (*uint32)(unsafe.Pointer(&q.buf[off]))
}

func (q *queue) producer() uint32 { return atomic.LoadUint32(q.index(queueProducer)) }
func (q *queue) consumer() uint32 { return atomic.LoadUint32(q.index(queueConsumer)) }

func (q *queue) setProducer(i uint32) { atomic.StoreUint32(q.index(queueProducer), i) }
func (q *queue) setConsumer(i uint32) { atomic.StoreUint32(q.index(queueConsumer), i) }

func (q *queue) full(prod uint32) bool { return q.consumer() == (prod+1)&q.mask }

// elem returns the element at index i.
func (q *queue) elem(i uint32) []byte {
	off := queueData + int(i&q.mask)<<q.log2Elem
	return q.buf[off : off+q.elemSize()]
}
