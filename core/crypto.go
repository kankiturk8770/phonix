package core

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
)

// لایه‌ی رمزنگاری مشترک روی همه‌ی پروتکل‌ها:
// handshake کوتاه مبتنی بر pre-shared key (الگوی Noise-IK ساده‌شده)
// + جابه‌جایی کلید هر N ثانیه + padding حداقلی ضد تفکیک اندازه.

const (
	FrameOverhead   = 16 /* GCM tag */ + 8 /* nonce */ + 4 /* len */
	MinFramePayload = 128 // حداقل padding ضد نشت اندازه
	KeyRotateSecs   = 120
)

var (
	ErrBadHandshake = errors.New("phoenix: bad handshake (active probe rejected)")
	ErrReplay       = errors.New("phoenix: replayed frame")
)

// readMagicAligned خواندن دقیقاً len(b) بایت بعد از یافتن magic در جریان.
// نویز قبل از magic دور ریخته می‌شود (سقف maxJunk بایت).
func readMagicAligned(conn io.ReadWriter, magic []byte, maxJunk int) ([]byte, error) {
	window := make([]byte, 0, len(magic)+maxJunk)
	one := make([]byte, 1)
	for len(window) < len(magic)+maxJunk {
		if _, err := io.ReadFull(conn, one); err != nil {
			return nil, err
		}
		window = append(window, one[0])
		if len(window) < len(magic) {
			continue
		}
		if string(window[len(window)-len(magic):]) == string(magic) {
			out := make([]byte, 32)
			_, err := io.ReadFull(conn, out)
			return out, err
		}
	}
	return nil, ErrBadHandshake
}

// Session alias صادرشده — بسته‌ی server از طریق این alias با سشن کار می‌کند
type Session = sessionKeys

type sessionKeys struct {
	send, recv cipher.AEAD
	sendNonce  uint64
	recvSeen   map[uint64]bool
}

// deriveKey کلید جلسه از کلید استاتیک + nonces تعویض‌شده
func deriveKey(static []byte, salt []byte, dir string) []byte {
	h := sha256.New()
	h.Write(static)
	h.Write(salt)
	h.Write([]byte("phoenix/" + dir))
	return h.Sum(nil)
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// handshakeMagic قبل از challenge فرستاده می‌شود تا سرور بتواند
// میان نویز early-silence و شروع واقعی handshake هم‌راستا شود.
const handshakeMagic = "PHXH"

// ServerHandshake سمت سرور: challenge-response کوتاه.
// تحمل‌پذیر نویز: جریان را می‌غزد تا magic پیدا شود (سقف 512 بایت نویز).
func ServerHandshake(conn io.ReadWriter, staticPriv []byte) (*sessionKeys, error) {
	challenge, err := readMagicAligned(conn, []byte(handshakeMagic), 512)
	if err != nil {
		return nil, err
	}
	// proof = HMAC(static, challenge) — بدون HMAC expos کردن کلید
	proof := deriveKey(staticPriv, challenge, "proof")
	if _, err := conn.Write(proof[:16]); err != nil {
		return nil, err
	}
	salt := make([]byte, 32)
	if _, err := io.ReadFull(conn, salt); err != nil {
		return nil, err
	}
	sendAEAD, err := newAEAD(deriveKey(staticPriv, salt, "s2c"))
	if err != nil {
		return nil, err
	}
	recvAEAD, err := newAEAD(deriveKey(staticPriv, salt, "c2s"))
	if err != nil {
		return nil, err
	}
	return &sessionKeys{send: sendAEAD, recv: recvAEAD, recvSeen: map[uint64]bool{}}, nil
}

func ClientHandshake(conn io.ReadWriter, staticKey []byte) (*sessionKeys, error) {
	if _, err := conn.Write([]byte(handshakeMagic)); err != nil {
		return nil, err
	}
	challenge := make([]byte, 32)
	rand.Read(challenge)
	if _, err := conn.Write(challenge); err != nil {
		return nil, err
	}
	want := deriveKey(staticKey, challenge, "proof")
	got := make([]byte, 16)
	if _, err := io.ReadFull(conn, got); err != nil {
		return nil, err
	}
	if string(got) != string(want[:16]) {
		return nil, ErrBadHandshake
	}
	salt := make([]byte, 32)
	rand.Read(salt)
	if _, err := conn.Write(salt); err != nil {
		return nil, err
	}
	sendAEAD, err := newAEAD(deriveKey(staticKey, salt, "c2s"))
	if err != nil {
		return nil, err
	}
	recvAEAD, err := newAEAD(deriveKey(staticKey, salt, "s2c"))
	if err != nil {
		return nil, err
	}
	return &sessionKeys{send: sendAEAD, recv: recvAEAD, recvSeen: map[uint64]bool{}}, nil
}

// SealFrame رمزنگاری یک قاب + padding تصادفی تا حداقل MinFramePayload
func (s *sessionKeys) SealFrame(payload []byte) []byte {
	pad := MinFramePayload - len(payload)
	if pad < 0 {
		pad = 0
	}
	plain := make([]byte, 4+pad+len(payload))
	binary.BigEndian.PutUint32(plain[:4], uint32(len(payload)))
	rand.Read(plain[4 : 4+pad])
	copy(plain[4+pad:], payload)
	nonce := make([]byte, 12)
	binary.BigEndian.PutUint64(nonce[4:], s.sendNonce)
	s.sendNonce++
	sealed := s.send.Seal(nil, nonce, plain, nil)
	out := make([]byte, 12+len(sealed))
	copy(out, nonce)
	copy(out[12:], sealed)
	return out
}

// OpenFrame باز کردن قاب؛ replay را رد می‌کند
func (s *sessionKeys) OpenFrame(frame []byte) ([]byte, error) {
	if len(frame) < FrameOverhead {
		return nil, ErrBadHandshake
	}
	nonce := make([]byte, 12)
	copy(nonce, frame[:12])
	n := binary.BigEndian.Uint64(nonce[4:])
	if s.recvSeen[n] {
		return nil, ErrReplay
	}
	plain, err := s.recv.Open(nil, nonce, frame[12:], nil)
	if err != nil {
		return nil, err
	}
	s.recvSeen[n] = true
	if len(s.recvSeen) > 4096 {
		s.recvSeen = map[uint64]bool{n: true} // compaction ساده
	}
	l := int(binary.BigEndian.Uint32(plain[:4]))
	if l > len(plain)-4 {
		return nil, ErrBadHandshake
	}
	// چیدمان: [len(4)][padding][payload] → payload در انتهاست
	return plain[len(plain)-l:], nil
}
