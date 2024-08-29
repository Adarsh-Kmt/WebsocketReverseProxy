package rwmutex

import "sync"

// a write preferred RWMutex. If writers are waiting to write, new readers cannot read, even though readers are currently reading.
type ReadWriteMutex struct {
	writersWaitingCount int
	readersReadingCount int
	writerActive        bool
	condVar             *sync.Cond
}

func InitializeReadWriteMutex() *ReadWriteMutex {

	return &ReadWriteMutex{condVar: sync.NewCond(&sync.Mutex{})}
}
func (rwl *ReadWriteMutex) ReadLock() {

	rwl.condVar.L.Lock()

	for rwl.writerActive || rwl.writersWaitingCount > 0 {
		rwl.condVar.Wait()
	}

	rwl.readersReadingCount++
	rwl.condVar.L.Unlock()
}

func (rwl *ReadWriteMutex) ReadUnlock() {

	rwl.condVar.L.Lock()
	rwl.readersReadingCount--

	if rwl.readersReadingCount == 0 {
		rwl.condVar.Broadcast()
	}

	rwl.condVar.L.Unlock()
}

func (rwl *ReadWriteMutex) WriteLock() {

	rwl.condVar.L.Lock()

	rwl.writersWaitingCount++

	for rwl.writerActive || rwl.readersReadingCount > 0 {
		rwl.condVar.Wait()
	}

	rwl.writersWaitingCount--
	rwl.writerActive = true
	rwl.condVar.L.Unlock()
}

func (rwl *ReadWriteMutex) WriteUnlock() {

	rwl.condVar.L.Lock()

	rwl.writerActive = false

	rwl.condVar.Broadcast()

	rwl.condVar.L.Unlock()
}
