package vncauth

import "testing"

func TestReverse(t *testing.T) {
	if reverse(0x01) != 0x80 || reverse(0x80) != 0x01 {
		t.Fatal(reverse(0x01), reverse(0x80))
	}
}

func TestResponseDeterministic(t *testing.T) {
	ch := make([]byte, 16)
	for i := range ch {
		ch[i] = byte(i + 1)
	}
	a, err := Response("secret", ch)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Response("secret", ch)
	if err != nil || string(a) != string(b) || len(a) != 16 {
		t.Fatalf("mismatch %x %x %v", a, b, err)
	}
	c, _ := Response("other", ch)
	if string(a) == string(c) {
		t.Fatal("password did not change the response")
	}
}
