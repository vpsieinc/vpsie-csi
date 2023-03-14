package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path"
	"syscall"

	"code.k9.ms/vpsie-csi/pkg/driver"
)

func main() {
	cfg := driver.Config{}

	flag.StringVar(&cfg.EndPoint, "endpoint", "unix://tmp/csi.sock", "CSI endpoint")
	flag.StringVar(&cfg.Token, "token", "", "vpsie access token")
	flag.StringVar(&cfg.ClientID, "clientID", "", "vpsie clint id")
	flag.StringVar(&cfg.ClientSecret, "clientSecret", "", "vpsie client secret ")
	flag.StringVar(&cfg.Url, "url", "", "url of the vpsie api")
	flag.StringVar(&cfg.DriverName, "driver-name", driver.DefaultDriverName, "Name for the driver.")
	flag.StringVar(&cfg.NodeID, "nodeid", "", "node id")

	showVersion := flag.Bool("version", false, "Show version.")

	flag.Parse()

	if *showVersion {
		baseName := path.Base(os.Args[0])
		fmt.Println(baseName, driver.GetVersion())
		os.Exit(0)
	}

	drvr, err := driver.NewDriver(&cfg)
	if err != nil {
		fmt.Printf("failed to create driver: %v\n", err.Error())
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-c
		cancel()
	}()

	if err := drvr.Run(ctx); err != nil {
		log.Fatalln(err)
	}
}
