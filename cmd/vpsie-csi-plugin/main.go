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
	"time"

	"code.k9.ms/vpsie-csi/pkg/driver"
	"github.com/robfig/cron"
)

func cleanupOldRecords() {
	driver.VolumeFailuresMu.Lock()
	defer driver.VolumeFailuresMu.Unlock()

	currentTime := time.Now()
	for volumeName, volume := range driver.VolumeFailures {
		if currentTime.Sub(volume.LastAccessed) >= driver.CleanupThreshold {
			delete(driver.VolumeFailures, volumeName)
		}
	}
}

func main() {
	cfg := driver.Config{}

	flag.StringVar(&cfg.EndPoint, "endpoint", "unix://tmp/csi.sock", "CSI endpoint")
	flag.StringVar(&cfg.Token, "token", "", "vpsie access token")
	flag.StringVar(&cfg.Url, "url", "", "url of the vpsie api")
	flag.StringVar(&cfg.DriverName, "driver-name", driver.DefaultDriverName, "Name for the driver.")
	flag.StringVar(&cfg.StorageTag, "storage-tag", "", "Tag Vpsie Storage on Create/Attach.")
	flag.IntVar(&cfg.MaxRetries, "max-retries", 3, "Maximum number of retries.")

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

	crn := cron.New()
	crn.AddFunc("@hourly", cleanupOldRecords)
	crn.Start()

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
