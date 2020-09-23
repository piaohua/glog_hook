// +build ignore

package main

import (
	"flag"
	"io/ioutil"
	"time"

	log "github.com/Sirupsen/logrus"
	hook "github.com/piaohua/glog_hook"
)

func failOnError(err error, msg string) {
	if err != nil {
		log.Fatalf("%s:%s", msg, err)
	}
}

func main() {
	var logLevel string
	flag.StringVar(&logLevel, "log_level", "debug", "log level")
	flag.Parse()

	//logger :=log.standardLogger()
	//logger.SetFormatter(hook.DefaultFormatter(log.Fields{}))
	hook.DefaultHook()
	level, err := log.ParseLevel(logLevel)
	if err != nil {
		log.Panic(err)
	}
	log.SetLevel(level)
	log.SetReportCaller(true)
	log.SetOutput(ioutil.Discard)

	var n int
	for n < 30 {
		log.Info("Congratulations!")
		log.Debug("Congratulations!")
		log.Trace("Congratulations!")
		log.Error("Congratulations!")
		log.Warn("Congratulations!")
		log.Warning("Congratulations!")
		time.Sleep(1000 * time.Millisecond)
		n++
	}
	log.Panic("Congratulations!")
	log.Fatal("Congratulations!")
	time.Sleep(5000 * time.Millisecond)
}
