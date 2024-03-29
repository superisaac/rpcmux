package main

import (
	"context"
	"flag"
	log "github.com/sirupsen/logrus"
	"github.com/superisaac/jsoff/net"
	"github.com/superisaac/rpcmux/app"
	"github.com/superisaac/rpcmux/cmd/cmdutil"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func StartServer() {
	flagset := flag.NewFlagSet("rpcmux", flag.ExitOnError)

	// bind address
	pBind := flagset.String("bind", "", "bind address, default is 127.0.0.1:6000")

	// logging flags
	pLogfile := flagset.String("log", "", "path to log output, default is stdout")

	pYamlConfig := flagset.String("config", "", "path to <server config.yml>")

	// parse command-line flags
	flagset.Parse(os.Args[1:])
	cmdutil.SetupLogger(*pLogfile)

	application := app.Application()

	if *pYamlConfig != "" {
		err := application.Config.Load(*pYamlConfig)
		if err != nil {
			log.Panicf("load config error %s", err)
		}
	}

	bind := *pBind
	if bind == "" {
		bind = application.Config.Server.Bind
	}

	if bind == "" {
		bind = "127.0.0.1:6000"
	}

	insecure := application.Config.Server.TLS == nil

	//rpcmuxCfg := serverConfig.(RPCMAPConfig)
	rootCtx := context.Background()

	go func() {
		sigChannel := make(chan os.Signal, 1)
		signal.Notify(sigChannel, os.Interrupt, syscall.SIGTERM)
		select {
		case <-sigChannel:
			log.Infof("application interrupted")
			application.Stop()
			time.Sleep(time.Second * 1)
			os.Exit(0)
		}
	}()

	// start default router
	_ = application.GetRouter("default")
	actor := app.NewActor()
	var handler http.Handler
	handler = jsoffnet.NewGatewayHandler(rootCtx, actor, insecure)
	handler = jsoffnet.NewAuthHandler(application.Config.Server.Auth, handler)
	log.Infof("rpcmux starts at %s with secureness %t", bind, !insecure)
	jsoffnet.ListenAndServe(rootCtx, bind, handler, application.Config.Server.TLS)
}

func main() {
	StartServer()
}
