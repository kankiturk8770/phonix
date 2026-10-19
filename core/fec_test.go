package core

import (
	"bytes"
	"testing"
)

func TestFECRecoverSingleLoss(t *testing.T) {
	k, n := 4, 6
	size := 64
	g := NewFECGroup(k, n, size)
	data := [][]byte{
		bytes.Repeat([]byte("aaaa"), 16),
		bytes.Repeat([]byte("bbbb"), 16),
		bytes.Repeat([]byte("cccc"), 16),
		bytes.Repeat([]byte("dddd"), 16),
	}
	// ساخت پاریت‌ها
	var parity [][]byte
	for _, d := range data {
		done, p := g.AddData(d)
		if done {
			parity = p
		}
	}
	if len(parity) != n-k {
		t.Fatalf("expected %d parities, got %d", n-k, len(parity))
	}
	// گروه تازه با یک قاب گم: داده‌ی شماره ۲
	g2 := NewFECGroup(k, n, size)
	for i, d := range data {
		if i == 2 {
			continue
		}
		g2.Add(i, d)
	}
	for j, p := range parity {
		g2.Add(k+j, p)
	}
	rec, ok := g2.Recover(2)
	if !ok {
		t.Fatal("recover failed")
	}
	if !bytes.Equal(rec[:len(data[2])], data[2]) {
		t.Fatal("recovered data mismatch")
	}
}

func TestTargetCodec(t *testing.T) {
	for _, addr := range []string{"1.2.3.4:80", "example.com:443", "[2001:db8::1]:53"} {
		b, err := EncodeTarget(addr)
		if err != nil {
			t.Fatalf("encode %s: %v", addr, err)
		}
		got, err := DecodeTarget(b)
		if err != nil {
			t.Fatalf("decode %s: %v", addr, err)
		}
		if got != addr {
			t.Fatalf("roundtrip: %s -> %s", addr, got)
		}
	}
}
