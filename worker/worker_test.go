package rpcmapworker

import (
	"context"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/superisaac/jsonz"
	"github.com/superisaac/jsonz/http"
	"github.com/superisaac/rpcmap/app"
	"io/ioutil"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if true {
		log.SetOutput(ioutil.Discard)
	}
	os.Exit(m.Run())
}

func TestWorker(t *testing.T) {
	assert := assert.New(t)

	rootCtx := context.Background()

	// start rpcmap server
	actor := app.NewActor()
	var handler http.Handler
	handler = jsonzhttp.NewGatewayHandler(rootCtx, actor, true)
	go jsonzhttp.ListenAndServe(rootCtx, "127.0.0.1:16001", handler)
	time.Sleep(100 * time.Millisecond)

	// prepare worker and connect to rpcmap server
	worker := NewServiceWorker([]string{"h2c://127.0.0.1:16001"})
	worker.OnTyped("echo", func(req *WorkerRequest, text string) (string, error) {
		return "echo: " + text, nil
	})
	go worker.ConnectWait(rootCtx)
	time.Sleep(100 * time.Millisecond)

	// create a request
	c, err := jsonzhttp.NewClient("http://127.0.0.1:16001")
	assert.Nil(err)

	reqmsg := jsonz.NewRequestMessage(1, "echo", []interface{}{"hi"})
	resmsg, err := c.Call(rootCtx, reqmsg)
	assert.Nil(err)
	assert.True(resmsg.IsResult())
	assert.Equal("echo: hi", resmsg.MustResult())
}

func TestRemoteServers(t *testing.T) {
	assert := assert.New(t)

	rootCtx := context.Background()

	app1 := app.NewApp()
	app1.Config.MQ.Urlstr = "redis://localhost:6379/7"
	app1.Config.Server.AdvertiseUrl = "http://127.0.0.1:16011"
	assert.Equal("redis", app1.Config.MQ.URL().Scheme)
	defer app1.Stop()

	app2 := app.NewApp()
	app2.Config.MQ.Urlstr = "redis://localhost:6379/7"
	app2.Config.Server.AdvertiseUrl = "http://127.0.0.1:16012"
	assert.Equal("redis", app2.Config.MQ.URL().Scheme)
	defer app2.Stop()

	// app1 server
	// start app1 server
	actor1 := app.NewActor(app1)
	_ = app1.GetRouter("default")
	var handler1 http.Handler
	handler1 = jsonzhttp.NewGatewayHandler(app1.Context(), actor1, true)
	go jsonzhttp.ListenAndServe(app1.Context(), "127.0.0.1:16011", handler1)
	time.Sleep(100 * time.Millisecond)

	// prepare worker and connect to app1
	workerCtx, cancelWorker := context.WithCancel(rootCtx)
	worker := NewServiceWorker([]string{"h2c://127.0.0.1:16011"})
	worker.OnTyped("echo", func(req *WorkerRequest, text string) (string, error) {
		return "echo: " + text, nil
	})
	go worker.ConnectWait(workerCtx)
	time.Sleep(100 * time.Millisecond)

	// start app2 server
	actor2 := app.NewActor(app2)
	_ = app2.GetRouter("default")
	var handler2 http.Handler
	handler2 = jsonzhttp.NewGatewayHandler(app2.Context(), actor2, true)
	go jsonzhttp.ListenAndServe(app2.Context(), "127.0.0.1:16012", handler2)
	time.Sleep(100 * time.Millisecond)

	// create a client to app2
	c, err := jsonzhttp.NewClient("http://127.0.0.1:16012")
	assert.Nil(err)

	// get provided methods the first time
	reqmethods1 := jsonz.NewRequestMessage(1, "rpc.methods", nil)
	methodsres1 := struct {
		Methods []string
		Remotes []string
	}{}
	err = c.UnwrapCall(rootCtx, reqmethods1, &methodsres1)
	assert.Nil(err)
	assert.Equal([]string{"echo"}, methodsres1.Remotes)

	// call the rpc method "echo"
	reqmsg := jsonz.NewRequestMessage(1, "echo", []interface{}{"hi"})
	resmsg, err := c.Call(rootCtx, reqmsg)
	assert.Nil(err)
	assert.True(resmsg.IsResult())
	assert.Equal("echo: hi", resmsg.MustResult())

	// stop worker
	cancelWorker()
	time.Sleep(100 * time.Millisecond)

	// get methods again
	reqmethods2 := jsonz.NewRequestMessage(1, "rpc.methods", nil)
	methodsres2 := struct {
		Methods []string
		Remotes []string
	}{}
	err = c.UnwrapCall(rootCtx, reqmethods2, &methodsres2)
	assert.Nil(err)
	assert.Equal([]string{}, methodsres2.Remotes)
}
