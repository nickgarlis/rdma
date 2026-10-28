//go:build linux

package rxe

import (
	"encoding/binary"
	"fmt"
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

// A queue is a struct rxe_queue_buf ring shared with the kernel, mapped from
// the device file. Its memory is only reachable inside do, under the
// mapping's lock, so it is never used while or after it is unmapped.
type queue struct {
	mem      *uverbs.Mapping
	log2Elem uint32
	mask     uint32
}

// newQueue maps the ring described by mi from the device file.
func newQueue(dev *rdma.Device, mi mmapInfo) (*queue, error) {
	if mi.Size < queueData {
		return nil, fmt.Errorf("rxe: queue of %d bytes is too small", mi.Size)
	}

	mem, err := dev.Conn().Mmap(int64(mi.Offset), int(mi.Size), uverbs.ProtRead|uverbs.ProtWrite)
	if err != nil {
		return nil, fmt.Errorf("rxe: map queue: %w", err)
	}

	// The ring's geometry is read once: the kernel never changes it, and
	// later changes to the shared memory must not move the bounds.
	q := &queue{mem: mem}
	if err := mem.Do(func(b []byte) error {
		q.log2Elem = binary.NativeEndian.Uint32(b[queueLog2ElemSize:])
		q.mask = binary.NativeEndian.Uint32(b[queueIndexMask:])

		if end := queueData + (uint64(q.mask)+1)<<q.log2Elem; end > uint64(len(b)) {
			return fmt.Errorf("rxe: queue of %d elements of %d bytes exceeds mapping of %d bytes",
				q.mask+1, q.elemSize(), len(b))
		}

		return nil
	}); err != nil {
		_ = mem.Close()
		return nil, err
	}

	return q, nil
}

// produce fills up to n free slots of the ring with fill, in order, and
// makes them visible to the kernel with a single producer update. It stops at
// the first fill error, or with errQueueFull when the ring runs full, and
// returns how many slots it produced: those are published even if a later
// one failed. It fails with uverbs.ErrClosed once the queue is closed.
func (q *queue) produce(n int, fill func(i int, slot []byte) error) (int, error) {
	done := 0
	err := q.mem.Do(func(b []byte) error {
		// The kernel only frees slots meanwhile, so a snapshot of its
		// consumer index can stop the batch early but never overrun it.
		prod := q.producer(b)
		cons := q.consumer(b)

		var err error
		for ; done < n; done++ {
			slot := (prod + uint32(done)) & q.mask
			if (slot+1)&q.mask == cons {
				err = errQueueFull
				break
			}

			if err = fill(done, q.elem(b, slot)); err != nil {
				break
			}
		}

		// One update publishes every slot filled so far.
		if done > 0 {
			q.setProducer(b, (prod+uint32(done))&q.mask)
		}

		return err
	})

	return done, err
}

// consume reads up to n slots the kernel produced with read, in order, and
// hands them back with a single consumer update. It returns how many it
// consumed; fewer than n means the ring ran empty, which is not an error. It
// fails with uverbs.ErrClosed once the queue is closed.
func (q *queue) consume(n int, read func(i int, slot []byte) error) (int, error) {
	done := 0
	err := q.mem.Do(func(b []byte) error {
		// The kernel only adds entries meanwhile, so a snapshot of its
		// producer index can stop the batch early but never overrun it.
		cons := q.consumer(b)
		prod := q.producer(b)

		var err error
		for ; done < n; done++ {
			slot := (cons + uint32(done)) & q.mask
			if slot == prod {
				break
			}

			if err = read(done, q.elem(b, slot)); err != nil {
				break
			}
		}

		if done > 0 {
			q.setConsumer(b, (cons+uint32(done))&q.mask)
		}

		return err
	})

	return done, err
}

// close unmaps the ring, after any running do call returns. Closing a closed
// queue does nothing.
func (q *queue) close() error { return q.mem.Close() }

func (q *queue) elemSize() int { return 1 << q.log2Elem }

// The helpers below work on the ring memory b inside produce and consume.
// Indices are read and written atomically; the kernel pairs them with acquire
// and release ordering, so slots are complete before an index update
// publishes them.

func index(b []byte, off int) *uint32 {
	return (*uint32)(unsafe.Pointer(&b[off]))
}

func (q *queue) producer(b []byte) uint32 { return atomic.LoadUint32(index(b, queueProducer)) }
func (q *queue) consumer(b []byte) uint32 { return atomic.LoadUint32(index(b, queueConsumer)) }

func (q *queue) setProducer(b []byte, i uint32) { atomic.StoreUint32(index(b, queueProducer), i) }
func (q *queue) setConsumer(b []byte, i uint32) { atomic.StoreUint32(index(b, queueConsumer), i) }

// elem returns the element at index i.
func (q *queue) elem(b []byte, i uint32) []byte {
	size := q.elemSize()
	off := queueData + int(i&q.mask)*size
	return b[off : off+size]
}
