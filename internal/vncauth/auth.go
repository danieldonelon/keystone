package vncauth

import "crypto/des"

func Response(password string, challenge []byte) ([]byte, error) {
	key := make([]byte, 8)
	copy(key, password)
	for i := range key {
		key[i] = reverse(key[i])
	}
	block, err := des.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 16)
	block.Encrypt(out[:8], challenge[:8])
	block.Encrypt(out[8:], challenge[8:16])
	return out, nil
}

func reverse(b byte) byte {
	var o byte
	for i := 0; i < 8; i++ {
		o <<= 1
		o |= b & 1
		b >>= 1
	}
	return o
}
