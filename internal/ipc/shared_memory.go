// shared_memory.go
//go:build windows

package ipc

import (
	"encoding/binary"
	"errors"
	"log"
	"runtime"
	"sync"
	"time"
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
	mu     sync.Mutex
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

// TryRead
//
//	ok=false, err=nil → timeout（正常系）
//	ok=true,  err=nil → 読み取り成功
//	err!=nil          → システムエラー
func (m *SharedMemory) TryRead(timeoutMs int) ([]byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := time.Now().UnixNano()

	log.Printf("[SHM] Read(%d) wait start", id)
	start := time.Now()

	// WaitForSingleObject と ReleaseMutex を同一 OS スレッドで行う
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	s, werr := windows.WaitForSingleObject(m.hMutex, uint32(timeoutMs))
	elapsed := time.Since(start)

	if werr != nil {
		log.Printf("[SHM] Read(%d) WaitForSingleObject error: %v (elapsed=%v)", id, werr, elapsed)
		return nil, false, werr
	}

	switch s {
	case windows.WAIT_OBJECT_0, windows.WAIT_ABANDONED:
		log.Printf("[SHM] Read(%d) wait ok (result=%#x, elapsed=%v)", id, s, elapsed)

	case uint32(windows.WAIT_TIMEOUT):
		log.Printf("[SHM] Read(%d) timeout (elapsed=%v)", id, elapsed)
		return nil, false, nil

	default:
		log.Printf("[SHM] Read(%d) unexpected wait result=%#x", id, s)
		return nil, false, errors.New("mutex wait failed")
	}

	defer func() {
		if err := windows.ReleaseMutex(m.hMutex); err != nil {
			log.Printf("[SHM] Read(%d) ReleaseMutex error: %v", id, err)
		} else {
			log.Printf("[SHM] Read(%d) released", id)
		}
	}()

	if m.size < HeaderSize {
		return nil, false, errors.New("invalid shared memory size")
	}

	payloadSize := binary.LittleEndian.Uint32(m.buf[0:4])
	if int(payloadSize) > m.size-HeaderSize {
		return nil, false, errors.New("payload too large")
	}

	if payloadSize == 0 {
		return nil, true, nil
	}

	data := make([]byte, payloadSize)
	copy(data, m.buf[4:4+payloadSize])
	return data, true, nil
}

func (m *SharedMemory) Read(timeoutMs int) ([]byte, error) {
	data, ok, err := m.TryRead(timeoutMs)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return data, nil
}

// TryWrite
//
//	ok=false, err=nil → timeout（正常系）
//	ok=true,  err=nil → 書き込み成功
//	err!=nil          → システムエラー
func (m *SharedMemory) TryWrite(data []byte, timeoutMs int) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(data)+HeaderSize > m.size {
		return false, errors.New("data too large")
	}

	id := time.Now().UnixNano()

	log.Printf("[SHM] Write(%d) wait start", id)
	start := time.Now()

	// WaitForSingleObject と ReleaseMutex を同一 OS スレッドで行う
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	s, werr := windows.WaitForSingleObject(m.hMutex, uint32(timeoutMs))
	elapsed := time.Since(start)

	if werr != nil {
		log.Printf("[SHM] Write(%d) WaitForSingleObject error: %v (elapsed=%v)", id, werr, elapsed)
		return false, werr
	}

	switch s {
	case windows.WAIT_OBJECT_0, windows.WAIT_ABANDONED:
		log.Printf("[SHM] Write(%d) wait ok (result=%#x, elapsed=%v)", id, s, elapsed)

	case uint32(windows.WAIT_TIMEOUT):
		log.Printf("[SHM] Write(%d) timeout (elapsed=%v)", id, elapsed)
		return false, nil

	default:
		log.Printf("[SHM] Write(%d) unexpected wait result=%#x", id, s)
		return false, errors.New("mutex wait failed")
	}

	defer func() {
		if err := windows.ReleaseMutex(m.hMutex); err != nil {
			log.Printf("[SHM] Write(%d) ReleaseMutex error: %v", id, err)
		} else {
			log.Printf("[SHM] Write(%d) released", id)
		}
	}()

	binary.LittleEndian.PutUint32(m.buf[0:4], uint32(len(data)))
	copy(m.buf[4:], data)
	return true, nil
}

func (m *SharedMemory) Write(data []byte, timeoutMs int) error {
	ok, err := m.TryWrite(data, timeoutMs)
	if err != nil {
		return err
	}
	// timeout は正常系なので err=nil のまま
	_ = ok
	return nil
}

func (m *SharedMemory) Close() {
	_ = unmapViewOfFile(m.view)
	_ = closeHandle(m.hMap)
	_ = closeHandle(m.hMutex)
}
