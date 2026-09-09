package indexer

import (
	"errors"
	"sync"

	"golang.org/x/sys/unix"
)

// fsnotify v1.9.0's kqueue backend scans children on directory writes, stops
// at a dangling sibling, and discards dirChange's error. Observe just the root
// vnode independently: no sibling opens, polling, or dependency modifications.
// nil notifications mean "rescan content roots"; non-nil errors are fatal.
func watchRootChanges(root string) (<-chan error, func(), error) {
	fd, err := unix.Open(root, unix.O_EVTONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	kq, err := unix.Kqueue()
	if err != nil {
		unix.Close(fd)
		return nil, nil, err
	}
	unix.CloseOnExec(kq)
	changes := []unix.Kevent_t{
		{Ident: uint64(fd), Filter: unix.EVFILT_VNODE, Flags: unix.EV_ADD | unix.EV_CLEAR, Fflags: unix.NOTE_WRITE | unix.NOTE_RENAME | unix.NOTE_DELETE | unix.NOTE_REVOKE},
		{Ident: 1, Filter: unix.EVFILT_USER, Flags: unix.EV_ADD | unix.EV_CLEAR},
	}
	if _, err := unix.Kevent(kq, changes, nil, nil); err != nil {
		unix.Close(kq)
		unix.Close(fd)
		return nil, nil, err
	}
	notifications := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer close(notifications)
		events := make([]unix.Kevent_t, 2)
		for {
			n, err := unix.Kevent(kq, nil, events, nil)
			if errors.Is(err, unix.EINTR) {
				continue
			}
			if err != nil {
				// Replace a coalesced change with the fatal error, never drop it.
				select {
				case <-notifications:
				default:
				}
				notifications <- err
				return
			}
			for _, event := range events[:n] {
				if event.Filter == unix.EVFILT_USER {
					return
				}
				select {
				case notifications <- nil:
				default: // Coalesce notifications; one scoped rescan covers them.
				}
			}
		}
	}()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			// Wake a blocked kevent without polling. FDs stay owned until the
			// reader exits, including its error path, preventing descriptor reuse.
			_, _ = unix.Kevent(kq, []unix.Kevent_t{{Ident: 1, Filter: unix.EVFILT_USER, Fflags: unix.NOTE_TRIGGER}}, nil, nil)
			<-done
			unix.Close(kq)
			unix.Close(fd)
		})
	}
	return notifications, stop, nil
}
