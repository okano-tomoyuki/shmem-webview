// winapi.go
//go:build windows

package ipc

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// ===== kernel32.dll =====

var (
	modkernel32            = windows.NewLazySystemDLL("kernel32.dll")
	procCreateFileMappingW = modkernel32.NewProc("CreateFileMappingW")
	procOpenFileMappingW   = modkernel32.NewProc("OpenFileMappingW")
	procMapViewOfFile      = modkernel32.NewProc("MapViewOfFile")
	procUnmapViewOfFile    = modkernel32.NewProc("UnmapViewOfFile")
	procCloseHandle        = modkernel32.NewProc("CloseHandle")
)

// ===== WinAPI ラッパ（外部非公開） =====

func createFileMapping(name string, size uint32) (windows.Handle, error) {
	r0, _, e1 := procCreateFileMappingW.Call(
		uintptr(windows.InvalidHandle),
		0,
		uintptr(windows.PAGE_READWRITE),
		0,
		uintptr(size),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(name))),
	)
	h := windows.Handle(r0)
	if h == 0 {
		return 0, e1
	}
	return h, nil
}

func openFileMapping(name string) (windows.Handle, error) {
	r0, _, e1 := procOpenFileMappingW.Call(
		uintptr(windows.FILE_MAP_WRITE),
		0,
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(name))),
	)
	h := windows.Handle(r0)
	if h == 0 {
		return 0, e1
	}
	return h, nil
}

func mapViewOfFile(h windows.Handle, size uintptr) (uintptr, error) {
	r0, _, e1 := procMapViewOfFile.Call(
		uintptr(h),
		uintptr(windows.FILE_MAP_WRITE),
		0, 0,
		size,
	)
	if r0 == 0 {
		return 0, e1
	}
	return r0, nil
}

func unmapViewOfFile(addr uintptr) error {
	r0, _, e1 := procUnmapViewOfFile.Call(addr)
	if r0 == 0 {
		return e1
	}
	return nil
}

func closeHandle(h windows.Handle) error {
	r0, _, e1 := procCloseHandle.Call(uintptr(h))
	if r0 == 0 {
		return e1
	}
	return nil
}
