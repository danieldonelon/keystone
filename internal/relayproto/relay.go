package relayproto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"time"
)

const (
	MagicDisc  = "KSD1"
	MagicRelay = "KSRL"
	header     = 4 + 32 + 32 + 32
	discLen    = 4 + 32 + 2 + 8 + 32
)

func SealRelay(secret []byte, src, dst [32]byte, payload []byte) []byte {
	buf := make([]byte, header+len(payload))
	copy(buf, MagicRelay)
	copy(buf[4:], src[:])
	copy(buf[36:], dst[:])
	copy(buf[header:], payload)
	mac := hmac.New(sha256.New, secret)
	mac.Write(buf[:68])
	mac.Write(buf[header:])
	copy(buf[68:header], mac.Sum(nil))
	return buf
}

func OpenRelay(secret, packet []byte) (src, dst [32]byte, payload []byte, ok bool) {
	if len(secret) == 0 || len(packet) < header || string(packet[:4]) != MagicRelay {
		return src, dst, nil, false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(packet[:68])
	mac.Write(packet[header:])
	if !hmac.Equal(mac.Sum(nil), packet[68:header]) {
		return src, dst, nil, false
	}
	copy(src[:], packet[4:36])
	copy(dst[:], packet[36:68])
	payload = append([]byte(nil), packet[header:]...)
	return src, dst, payload, true
}

func SealDisc(secret []byte, pub [32]byte, listenPort uint16) []byte {
	buf := make([]byte, discLen)
	copy(buf, MagicDisc)
	copy(buf[4:], pub[:])
	binary.BigEndian.PutUint16(buf[36:38], listenPort)
	binary.BigEndian.PutUint64(buf[38:46], uint64(time.Now().Unix()))
	mac := hmac.New(sha256.New, secret)
	mac.Write(buf[:46])
	copy(buf[46:], mac.Sum(nil))
	return buf
}

func OpenDisc(secret, packet []byte, now time.Time) (pub [32]byte, listenPort uint16, ok bool) {
	if len(secret) == 0 || len(packet) != discLen || string(packet[:4]) != MagicDisc {
		return pub, 0, false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(packet[:46])
	if !hmac.Equal(mac.Sum(nil), packet[46:]) {
		return pub, 0, false
	}
	ts := int64(binary.BigEndian.Uint64(packet[38:46]))
	if ts != 0 && (now.Unix()-ts > 600 || ts-now.Unix() > 600) {
		return pub, 0, false
	}
	copy(pub[:], packet[4:36])
	return pub, binary.BigEndian.Uint16(packet[36:38]), true
}

func DecodeKey(s string) (out [32]byte, ok bool) {
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 32 {
		return out, false
	}
	copy(out[:], b)
	return out, true
}

func EncodeKey(k [32]byte) string { return hex.EncodeToString(k[:]) }
