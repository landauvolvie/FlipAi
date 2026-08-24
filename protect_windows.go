//go:build windows

package main

import (
	"errors"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// Windows protects saved credentials with DPAPI. User scope is the default and
// the safer choice: only this signed-in account can read them. Machine scope is
// used only when FlipAi is set to start before sign-in, because a task that
// runs without an interactive logon has no user key to decrypt with.
const (
	cryptProtectUIForbidden  = 0x1
	cryptProtectLocalMachine = 0x4
)

var machineScopeSecrets atomic.Bool

func setSecretScope(machine bool) { machineScopeSecrets.Store(machine) }
func secretScopeIsMachine() bool  { return machineScopeSecrets.Load() }

type dataBlob struct {
	cbData uint32
	pbData *byte
}

var crypt32 = syscall.NewLazyDLL("crypt32.dll")
var kernel32 = syscall.NewLazyDLL("kernel32.dll")
var cryptProtectData = crypt32.NewProc("CryptProtectData")
var cryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
var localFree = kernel32.NewProc("LocalFree")

func blobFromBytes(b []byte) dataBlob {
	if len(b) == 0 {
		return dataBlob{}
	}
	return dataBlob{uint32(len(b)), &b[0]}
}
func blobBytes(b dataBlob) []byte {
	if b.cbData == 0 || b.pbData == nil {
		return nil
	}
	src := unsafe.Slice(b.pbData, b.cbData)
	out := append([]byte(nil), src...)
	localFree.Call(uintptr(unsafe.Pointer(b.pbData)))
	return out
}
func protect(in []byte) ([]byte, error) {
	ib := blobFromBytes(in)
	var ob dataBlob
	flags := uintptr(cryptProtectUIForbidden)
	if secretScopeIsMachine() {
		flags |= cryptProtectLocalMachine
	}
	r, _, e := cryptProtectData.Call(uintptr(unsafe.Pointer(&ib)), 0, 0, 0, 0, flags, uintptr(unsafe.Pointer(&ob)))
	if r == 0 {
		return nil, e
	}
	return blobBytes(ob), nil
}
func unprotect(in []byte) ([]byte, error) {
	if len(in) == 0 {
		return nil, errors.New("empty protected data")
	}
	ib := blobFromBytes(in)
	var ob dataBlob
	// Unprotect does not need the scope: a DPAPI blob records its own.
	r, _, e := cryptUnprotectData.Call(uintptr(unsafe.Pointer(&ib)), 0, 0, 0, 0, cryptProtectUIForbidden, uintptr(unsafe.Pointer(&ob)))
	if r == 0 {
		return nil, e
	}
	return blobBytes(ob), nil
}
