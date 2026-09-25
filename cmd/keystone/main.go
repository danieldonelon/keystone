package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"runtime"
	"strconv"

	"keystone/internal/config"
	"keystone/internal/coord"
	"keystone/internal/daemon"
	"keystone/internal/identity"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "init":
		err = doInit(os.Args[2:])
	case "join":
		err = doJoin(os.Args[2:])
	case "up":
		err = doUp(os.Args[2:])
	case "doctor":
		err = daemon.Doctor(context.Background())
	case "status":
		err = daemon.Status()
	case "version":
		fmt.Println("keystone", config.Version)
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "keystone:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `Keystone %s — a private mesh for your own computers

  keystone init --name laptop
  keystone join --name pi --coordinator https://HOST:7707 --token TOKEN --pin PIN
  keystone up
  keystone doctor
  keystone status

The first computer runs init and stays up as the coordinator.
Every other computer, including a Raspberry Pi, runs join and then up.
`, config.Version)
}

func doInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	name := fs.String("name", hostname(), "name of this computer")
	share := fs.String("share", config.DefaultShareRoot(), "folder other computers can browse")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if _, err := os.Stat(config.Path()); err == nil {
		return fmt.Errorf("a profile already exists at %s", config.Path())
	}
	priv, pub, err := newKey()
	if err != nil {
		return err
	}
	token, err := config.NewToken()
	if err != nil {
		return err
	}
	secret, err := config.NewSecret()
	if err != nil {
		return err
	}
	var ips []net.IP
	for _, ip := range config.PrivateIPv4s() {
		ips = append(ips, net.ParseIP(ip.String()))
	}
	pin, err := config.GenerateCert(ips)
	if err != nil {
		return err
	}
	host := config.BestAdvertiseIP()
	url := "https://" + net.JoinHostPort(host, strconv.Itoa(config.CoordPort))
	cfg := config.File{
		Version: 1, Name: *name, Role: "coordinator",
		PrivateKey: priv, PublicKey: pub, MeshIP: config.MeshIP(1),
		ListenPort: config.WGPort, CoordinatorURL: url, Token: token, Pin: pin,
		NetworkSecret: secret, ShareRoot: *share, WebPort: config.WebPort,
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	st, err := coord.LoadState(config.StatePath())
	if err != nil {
		return err
	}
	if _, err := st.Allocate(pub, *name, runtime.GOOS, config.WGPort, nil, runtime.GOOS == "windows"); err != nil {
		return err
	}
	fmt.Printf("Keystone coordinator %q is ready.\n", *name)
	fmt.Printf("Shared folder: %s\n", *share)
	fmt.Printf("On each other computer:\n  keystone join --name pi --coordinator %s --token %s --pin %s\n", url, token, pin)
	fmt.Println("Then run: keystone up")
	return nil
}

func doJoin(args []string) error {
	fs := flag.NewFlagSet("join", flag.ContinueOnError)
	name := fs.String("name", hostname(), "name of this computer")
	coordinator := fs.String("coordinator", "", "coordinator URL")
	token := fs.String("token", "", "join token from keystone init")
	pin := fs.String("pin", "", "certificate pin from keystone init")
	share := fs.String("share", config.DefaultShareRoot(), "folder other computers can browse")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *coordinator == "" || *token == "" || *pin == "" {
		return fmt.Errorf("join requires --coordinator, --token, and --pin")
	}
	priv, pub, err := newKey()
	if err != nil {
		return err
	}
	c := &coord.Client{Base: *coordinator, Token: *token, Pin: *pin}
	resp, err := c.Enroll(context.Background(), map[string]any{
		"name": *name, "public_key": pub, "listen_port": config.WGPort,
		"os": runtime.GOOS, "lan": config.PrivateIPv4s(), "vnc": runtime.GOOS == "windows",
	})
	if err != nil {
		return err
	}
	meshIP, _ := resp["mesh_ip"].(string)
	secret, _ := resp["network_secret"].(string)
	if meshIP == "" || secret == "" {
		return fmt.Errorf("coordinator returned an incomplete enrollment")
	}
	cfg := config.File{
		Version: 1, Name: *name, Role: "member",
		PrivateKey: priv, PublicKey: pub, MeshIP: meshIP,
		ListenPort: config.WGPort, CoordinatorURL: *coordinator, Token: *token, Pin: *pin,
		NetworkSecret: secret, ShareRoot: *share, WebPort: config.WebPort,
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Printf("Joined as %q at %s.\nRun: keystone up\n", *name, meshIP)
	return nil
}

func doUp(args []string) error {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	noBrowser := fs.Bool("no-browser", false, "do not open the Keystone window")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return daemon.Up(ctx, !*noBrowser)
}

func newKey() (string, string, error) {
	priv, err := identity.NewPrivate()
	if err != nil {
		return "", "", err
	}
	pub, err := priv.Public()
	if err != nil {
		return "", "", err
	}
	if _, err := hex.DecodeString(pub.Hex()); err != nil {
		return "", "", err
	}
	return priv.Hex(), pub.Hex(), nil
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "keystone"
	}
	return h
}
