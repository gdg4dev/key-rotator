package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var version = "1.0.0"

func main() {
	// `keyrotator <command> ...` runs the management CLI - with or without
	// leading global flags. Anything else (or no argument) starts the server.
	if wantsCLI(os.Args[1:]) {
		os.Exit(runCLI(os.Args[1:]))
	}

	var (
		cfgPath  = flag.String("config", "", `config file path (default: %APPDATA%\KeyRotator\config.json)`)
		listen   = flag.String("listen", "", "override listen address, e.g. 127.0.0.1:8787")
		svc      = flag.String("service", "", "Windows service control: install | uninstall | start | stop | restart | status | run")
		svcRun   = flag.Bool("service-run", false, "run under the Windows service dispatcher (used by the SCM)")
		openUI   = flag.Bool("open", false, "open the admin UI in the default browser")
		showVer  = flag.Bool("version", false, "print version and exit")
		noBanner = flag.Bool("quiet", false, "suppress the startup banner")
	)
	flag.Parse()

	if *showVer {
		fmt.Println("keyrotator", version)
		return
	}

	if *svc != "" {
		if handleServiceCommand(*svc) {
			return
		}
	}

	path := *cfgPath
	if path == "" {
		path = defaultConfigPath()
	}

	app, err := NewApp(path, *listen)
	if err != nil {
		log.Fatalf("init: %v", err)
	}

	if *svcRun {
		if err := runAsService(app); err != nil {
			log.Fatalf("service: %v", err)
		}
		return
	}

	stop := make(chan struct{})
	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
		<-ch
		close(stop)
	}()

	if *openUI {
		go func() {
			time.Sleep(600 * time.Millisecond)
			openBrowser(app.URL())
		}()
	}
	if !*noBanner {
		// Run logs the address once bound.
		log.Printf("starting keyrotator %s ...", version)
	}
	if err := app.Run(stop); err != nil {
		log.Fatal(err)
	}
}
