package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"hash"
)

func newHMAC(key []byte) hash.Hash { return hmac.New(sha256.New, key) }

var (
	errShort   = errors.New("dns: short packet")
	errBadName = errors.New("dns: bad name")
)

// randomRead پرکردن بافر با entropy سیستم
func randomRead(b []byte) {
	rand.Read(b)
}
