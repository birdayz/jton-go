//go:build !amd64

package jton

// On non-amd64 the scanners are scalar only.

func escapeIndex(b []byte) int { return escapeIndexScalar(b) }

func scanStringBody(b []byte) int { return scanStringScalar(b) }
