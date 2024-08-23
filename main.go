package main

import (
	"flag"
	"fmt"
	"net"
	"strings"
	"time"

	"code.dogecoin.org/dkm/internal/store"
	"code.dogecoin.org/dkm/internal/web"
	"code.dogecoin.org/gossip/dnet"
	"code.dogecoin.org/governor"
)

const WebAPIDefaultPort = 8089
const DBFilePath = "storage/dkm.db"

func main() {
	var bind dnet.Address
	flag.Func("bind", "<ip>:<port> (use [<ip>]:<port> for IPv6)", func(arg string) error {
		addr, err := parseIPPort(arg, "bind", WebAPIDefaultPort)
		if err != nil {
			return err
		}
		bind = addr
		return nil
	})
	flag.Parse()
	if !bind.IsValid() {
		bind = dnet.Address{Host: net.IPv4zero, Port: WebAPIDefaultPort}
	}

	gov := governor.New().CatchSignals().Restart(1 * time.Second)
	db, err := store.New(DBFilePath)
	if err != nil {
		panic(err)
	}

	// start the web server.
	gov.Add("dkm", web.New(bind, db))

	// run services until interrupted.
	gov.Start()
	gov.WaitForShutdown()
	fmt.Println("finished.")
}

// Parse an IPv4 or IPv6 address with optional port.
func parseIPPort(arg string, name string, defaultPort uint16) (dnet.Address, error) {
	// net.SplitHostPort doesn't return a specific error code,
	// so we need to detect if the port it present manually.
	colon := strings.LastIndex(arg, ":")
	bracket := strings.LastIndex(arg, "]")
	if colon == -1 || (arg[0] == '[' && bracket != -1 && colon < bracket) {
		ip := net.ParseIP(arg)
		if ip == nil {
			return dnet.Address{}, fmt.Errorf("bad --%v: invalid IP address: %v (use [<ip>]:port for IPv6)", name, arg)
		}
		return dnet.Address{
			Host: ip,
			Port: defaultPort,
		}, nil
	}
	res, err := dnet.ParseAddress(arg)
	if err != nil {
		return dnet.Address{}, fmt.Errorf("bad --%v: invalid IP address: %v (use [<ip>]:port for IPv6)", name, arg)
	}
	return res, nil
}
