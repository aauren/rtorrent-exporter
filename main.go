package main

import (
	"os"

	"github.com/aauren/rtorrent-exporter/cmd"
	"k8s.io/klog/v2"
)

func main() {
	if err := cmd.Execute(); err != nil {
		klog.Errorf("error executing rtorrent-exporter: %v", err)
		// klog.Flush() can't be deferred here, because os.Exit doesn't run deferred functions
		klog.Flush()
		os.Exit(1)
	}
	klog.Flush()
}
