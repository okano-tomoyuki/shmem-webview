// shared_memory.go
//go:build windows

package ipc

import (
	"encoding/binary"
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"

	"shmem-webview/internal/config"
)

const HeaderSize = 4

type SharedMemory struct {
	hMap   windows.Handle
	hMutex windows.Handle
	view   uintptr
	size   int
	buf    []byte
}

func OpenSharedMemory(cfg *config.ServerConfig, create bool) (*SharedMemory, error) {
	var hMap windows.Handle
	var err error

	if create {
		hMap, err = createFileMapping(cfg.ShmemName, uint32(cfg.ShmemSize))
	} else {
		hMap, err = openFileMapping(cfg.ShmemName)
	}
	if err != nil {
		return nil, err
	}

	// Mutex（存在していても成功扱い）
	hMutex, err := windows.CreateMutex(nil, false, windows.StringToUTF16Ptr(cfg.MutexName))
	if err != nil && err != windows.ERROR_ALREADY_EXISTS {
		return nil, err
	}

	view, err := mapViewOfFile(hMap, uintptr(cfg.ShmemSize))
	if err != nil {
		return nil, err
	}

	buf := unsafe.Slice((*byte)(unsafe.Pointer(view)), cfg.ShmemSize)

	return &SharedMemory{
		hMap:   hMap,
		hMutex: hMutex,
		view:   view,
		size:   cfg.ShmemSize,
		buf:    buf,
	}, nil
}

func (m *SharedMemory) Read(timeoutMs int) ([]byte, error) {
	s, err := windows.WaitForSingleObject(m.hMutex, uint32(timeoutMs))
	if err != nil {
		return nil, err
	}
	if s != windows.WAIT_OBJECT_0 && s != windows.WAIT_ABANDONED {
		// TIMEOUT など
		return nil, errors.New("mutex wait timeout or failed")
	}
	defer windows.ReleaseMutex(m.hMutex)

	if m.size < HeaderSize {
		return nil, errors.New("invalid shared memory size")
	}

	payloadSize := binary.LittleEndian.Uint32(m.buf[0:4])
	if int(payloadSize) > m.size-HeaderSize {
		return nil, errors.New("payload too large")
	}

	data := make([]byte, payloadSize)
	copy(data, m.buf[4:4+payloadSize])
	return data, nil
}

func (m *SharedMemory) Write(data []byte, timeoutMs int) error {
	if len(data)+HeaderSize > m.size {
		return errors.New("data too large")
	}

	s, err := windows.WaitForSingleObject(m.hMutex, uint32(timeoutMs))
	if err != nil {
		return err
	}
	if s != windows.WAIT_OBJECT_0 && s != windows.WAIT_ABANDONED {
		return errors.New("mutex wait timeout or failed")
	}
	defer windows.ReleaseMutex(m.hMutex)

	binary.LittleEndian.PutUint32(m.buf[0:4], uint32(len(data)))
	copy(m.buf[4:], data)
	return nil
}

func (m *SharedMemory) Close() {
	unmapViewOfFile(m.view)
	closeHandle(m.hMap)
	closeHandle(m.hMutex)
}
