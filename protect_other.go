//go:build !windows

package main

func protect(in []byte) ([]byte, error)   { return in, nil }
func unprotect(in []byte) ([]byte, error) { return in, nil }
