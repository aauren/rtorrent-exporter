package main

import (
	"github.com/aauren/rtorrent-exporter/cmd"
	"k8s.io/klog/v2"
)

func main() {
	err := cmd.Execute()
	if err != nil {
		klog.Errorf("error executing rtorrent-exporter: %v", err)
	}
}
