//go:build !windows

package main

func protect(in []byte) ([]byte, error)   { return in, nil }
func unprotect(in []byte) ([]byte, error) { return in, nil }

// Secret scope is a Windows DPAPI concept; elsewhere these are no-ops so the
// settings path compiles and can be tested.
func setSecretScope(machine bool) {}
func secretScopeIsMachine() bool  { return false }
