package core

import (
	"crypto/cipher"
	"crypto/sha256"
	"encoding/binary"
)

// رمزنگاری PhoenixUDP: کلیدهای جهت‌دار از کلید مشترک + salt کلاینت + salt سرور.
// هر DATA با AES-256-GCM و nonce = sess(4) || seq(4) || 0000 رمز می‌شود؛
// بازارسال همان متن رمز را می‌فرستد (nonce تکراری با متن یکسان امن است).

// PUDPKeys کلیدهای c2s و s2c
func PUDPKeys(static, clientSalt, serverSalt []byte) (c2s, s2c cipher.AEAD, err error) {
	derive := func(dir string) []byte {
		h := sha256.New()
		h.Write(static)
		h.Write(clientSalt)
		h.Write(serverSalt)
		h.Write([]byte("phoenix-udp/" + dir))
		return h.Sum(nil)
	}
	if c2s, err = newAEAD(derive("c2s")); err != nil {
		return
	}
	s2c, err = newAEAD(derive("s2c"))
	return
}

// PUDPNonce nonce یکتا برای (سشن، seq) در هر جهت
func PUDPNonce(sess, seq uint32) []byte {
	n := make([]byte, 12)
	binary.BigEndian.PutUint32(n[0:4], sess)
	binary.BigEndian.PutUint32(n[4:8], seq)
	return n
}
