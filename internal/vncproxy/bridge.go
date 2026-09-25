package vncproxy

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"

	"keystone/internal/vncauth"
)

func Bridge(client, server net.Conn, password string) error {
	defer client.Close()
	defer server.Close()
	initMsg, err := frontServer(server, password)
	if err != nil {
		return err
	}
	if err := frontClient(client, initMsg); err != nil {
		return err
	}
	errc := make(chan error, 2)
	go func() { _, e := io.Copy(server, client); errc <- e }()
	go func() { _, e := io.Copy(client, server); errc <- e }()
	return <-errc
}

func frontServer(conn net.Conn, password string) ([]byte, error) {
	ver := make([]byte, 12)
	if _, err := io.ReadFull(conn, ver); err != nil {
		return nil, err
	}
	if _, err := conn.Write([]byte("RFB 003.008\n")); err != nil {
		return nil, err
	}
	var nsec [1]byte
	if _, err := io.ReadFull(conn, nsec[:]); err != nil {
		return nil, err
	}
	if nsec[0] == 0 {
		var ln [4]byte
		if _, err := io.ReadFull(conn, ln[:]); err != nil {
			return nil, err
		}
		reason := make([]byte, binary.BigEndian.Uint32(ln[:]))
		_, _ = io.ReadFull(conn, reason)
		return nil, fmt.Errorf("vnc refused the connection: %s", reason)
	}
	types := make([]byte, nsec[0])
	if _, err := io.ReadFull(conn, types); err != nil {
		return nil, err
	}
	choice := byte(0)
	for _, t := range types {
		if t == 1 {
			choice = 1
			break
		}
		if t == 2 {
			choice = 2
		}
	}
	if choice == 0 {
		return nil, fmt.Errorf("vnc server has no supported security type")
	}
	if choice == 2 && password == "" {
		return nil, fmt.Errorf("this screen requires a VNC password")
	}
	if _, err := conn.Write([]byte{choice}); err != nil {
		return nil, err
	}
	if choice == 2 {
		challenge := make([]byte, 16)
		if _, err := io.ReadFull(conn, challenge); err != nil {
			return nil, err
		}
		resp, err := vncauth.Response(password, challenge)
		if err != nil {
			return nil, err
		}
		if _, err := conn.Write(resp); err != nil {
			return nil, err
		}
	}
	var result [4]byte
	if _, err := io.ReadFull(conn, result[:]); err != nil {
		return nil, err
	}
	if binary.BigEndian.Uint32(result[:]) != 0 {
		return nil, fmt.Errorf("vnc authentication failed")
	}
	if _, err := conn.Write([]byte{1}); err != nil {
		return nil, err
	}
	hdr := make([]byte, 24)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		return nil, err
	}
	nameLen := binary.BigEndian.Uint32(hdr[20:24])
	name := make([]byte, nameLen)
	if _, err := io.ReadFull(conn, name); err != nil {
		return nil, err
	}
	return append(hdr, name...), nil
}

func frontClient(conn net.Conn, serverInit []byte) error {
	if _, err := conn.Write([]byte("RFB 003.008\n")); err != nil {
		return err
	}
	ver := make([]byte, 12)
	if _, err := io.ReadFull(conn, ver); err != nil {
		return err
	}
	if _, err := conn.Write([]byte{1, 1}); err != nil {
		return err
	}
	choice := make([]byte, 1)
	if _, err := io.ReadFull(conn, choice); err != nil {
		return err
	}
	if _, err := conn.Write([]byte{0, 0, 0, 0}); err != nil {
		return err
	}
	shared := make([]byte, 1)
	if _, err := io.ReadFull(conn, shared); err != nil {
		return err
	}
	_, err := conn.Write(serverInit)
	return err
}
