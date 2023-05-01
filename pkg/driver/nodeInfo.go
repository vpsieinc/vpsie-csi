package driver

import (
	"fmt"
	"os"

	"k8s.io/klog/v2"
)

func printNodeInfo() {
	// creates the in-cluster config

	hostName := os.Getenv("HOSTNAME")
	hostIP := os.Getenv("HOSTIP")
	klog.Infof("Hostname: %s\n", hostName)
	klog.Infof("IP address: %s\n", hostIP)
	
	fmt.Printf("Hostname: %s\n", hostName)
	fmt.Printf("IP address: %s\n", hostIP)
}
