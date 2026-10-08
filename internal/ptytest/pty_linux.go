//go:build linux

// Package ptytest allocates pseudo-terminals so tests can exercise the code
// paths that only run on a real terminal.
package ptytest

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// Open returns a connected master/slave pseudo-terminal pair.
func Open() (master, slave *os.File, err error) {
	master, err = os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, err
	}

	var unlock int32
	if err := ioctl(master.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); err != nil {
		master.Close()
		return nil, nil, err
	}

	var number uint32
	if err := ioctl(master.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&number))); err != nil {
		master.Close()
		return nil, nil, err
	}

	slave, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		return nil, nil, err
	}
	return master, slave, nil
}

// Capture collects everything write emits on the pseudo-terminal.
//
// A pty master never reports EOF while a slave is open, so the reader starts
// first and blocks until Capture closes the slave, which makes it fail with
// EIO. Starting the reader before write also keeps the pty buffer from filling
// up and deadlocking a large write.
func Capture(master, slave *os.File, write func() error) (string, error) {
	var out []byte
	finished := make(chan struct{})

	go func() {
		defer close(finished)
		buf := make([]byte, 64*1024)
		for {
			n, err := syscall.Read(int(master.Fd()), buf)
			if n > 0 {
				out = append(out, buf[:n]...)
			}
			if err != nil {
				return
			}
		}
	}()

	writeErr := write()
	_ = slave.Close()
	<-finished

	return string(out), writeErr
}

func ioctl(fd, request, arg uintptr) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, request, arg); errno != 0 {
		return errno
	}
	return nil
}
