package identity

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"golang.org/x/crypto/curve25519"
)

type Key struct{ b [32]byte }

func NewPrivate() (Key, error) {
	var k Key
	if _, err := rand.Read(k.b[:]); err != nil {
		return Key{}, err
	}
	k.b[0] &= 248
	k.b[31] = (k.b[31] & 127) | 64
	return k, nil
}

func Parse(s string) (Key, error) {
	raw, err := hex.DecodeString(s)
	if err != nil || len(raw) != 32 {
		return Key{}, fmt.Errorf("key must be 32 bytes of hex")
	}
	var k Key
	copy(k.b[:], raw)
	return k, nil
}

func (k Key) Hex() string { return hex.EncodeToString(k.b[:]) }

func (k Key) Public() (Key, error) {
	pub, err := curve25519.X25519(k.b[:], curve25519.Basepoint)
	if err != nil {
		return Key{}, err
	}
	var out Key
	copy(out.b[:], pub)
	return out, nil
}

func (k Key) Bytes() []byte {
	b := make([]byte, 32)
	copy(b, k.b[:])
	return b
}
