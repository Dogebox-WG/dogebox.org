package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path"
	"strings"
	"time"

	"code.dogecoin.org/dkm/internal"
	"code.dogecoin.org/dkm/internal/keymgr"
	"code.dogecoin.org/dkm/internal/store"
	"code.dogecoin.org/dkm/internal/web"
	"code.dogecoin.org/governor"
)

const WebAPIDefaultPort = 8089
const DBFileName = "dkm.db"

func main() {
	var bind internal.Address
	dir := "."
	stderr := log.New(os.Stderr, "", 0)
	flag.Func("dir", "<path> - storage directory (default '.')", func(arg string) error {
		ent, err := os.Stat(arg)
		if err != nil {
			stderr.Fatalf("--dir: %v", err)
		}
		if !ent.IsDir() {
			stderr.Fatalf("--dir: not a directory: %v", arg)
		}
		dir = arg
		return nil
	})
	flag.Func("bind", "<ip>:<port> (use [<ip>]:<port> for IPv6)", func(arg string) error {
		addr, err := parseIPPort(arg, "bind", WebAPIDefaultPort)
		if err != nil {
			stderr.Fatalf("%v", err)
		}
		bind = addr
		return nil
	})
	flag.Parse()
	if !bind.IsValid() {
		bind = internal.Address{Host: net.IPv4zero, Port: WebAPIDefaultPort}
	}

	gov := governor.New().CatchSignals().Restart(1 * time.Second)
	db, err := store.New(path.Join(dir, DBFileName))
	if err != nil {
		panic(err)
	}

	km := keymgr.New(db.WithCtx(gov.GlobalContext()))

	// start the web server.
	gov.Add("dkm", web.New(bind, db, km))

	// run services until interrupted.
	gov.Start()
	gov.WaitForShutdown()
	fmt.Println("finished.")
}

// Parse an IPv4 or IPv6 address with optional port.
func parseIPPort(arg string, name string, defaultPort uint16) (internal.Address, error) {
	// net.SplitHostPort doesn't return a specific error code,
	// so we need to detect if the port it present manually.
	colon := strings.LastIndex(arg, ":")
	bracket := strings.LastIndex(arg, "]")
	if colon == -1 || (arg[0] == '[' && bracket != -1 && colon < bracket) {
		ip := net.ParseIP(arg)
		if ip == nil {
			return internal.Address{}, fmt.Errorf("bad --%v: invalid IP address: %v (use [<ip>]:port for IPv6)", name, arg)
		}
		return internal.Address{
			Host: ip,
			Port: defaultPort,
		}, nil
	}
	res, err := internal.ParseAddress(arg)
	if err != nil {
		return internal.Address{}, fmt.Errorf("bad --%v: invalid IP address: %v (use [<ip>]:port for IPv6)", name, arg)
	}
	return res, nil
}
