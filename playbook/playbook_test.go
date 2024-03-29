package playbook

import (
	//"fmt"
	"context"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/superisaac/jsoff"
	"github.com/superisaac/jsoff/net"
	"github.com/superisaac/rpcmux/app"
	"io/ioutil"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	log.SetOutput(ioutil.Discard)
	os.Exit(m.Run())
}

const PbSay = `
---

version: 1.0.0
methods:
  say:
    shell:
      command: jq '"echo " + .params[0]'
      env:
        - "AA=BB"
    schema:
      type: 'method'
      description: say somthing using jq
      params:
        - type: 'string'
      returns:
        type: 'string'
`

const PbEndpoint = `
---

version: 1.0.0
methods:
  say:
    endpoint:
      url: http://127.0.0.1:16004
    schema:
      type: 'method'
      description: say somthing from a dedicated api server
      params:
        - type: 'string'
      returns:
        type: 'string'
`

func TestPlaybook(t *testing.T) {
	assert := assert.New(t)

	rootCtx := context.Background()

	// start rpcmux server
	actor := app.NewActor()
	var handler http.Handler
	handler = jsoffnet.NewGatewayHandler(rootCtx, actor, true)
	go jsoffnet.ListenAndServe(rootCtx, "127.0.0.1:16002", handler)
	time.Sleep(100 * time.Millisecond)

	// create playbook instance and run
	pb := NewPlaybook()
	err := pb.Config.LoadBytes([]byte(PbSay))
	assert.Nil(err)

	method, ok := pb.Config.Methods["say"]
	assert.True(ok)
	assert.NotNil(method.innerSchema)
	assert.Equal("method", method.innerSchema.Type())

	go func() {
		err := pb.Run(rootCtx, "h2c://127.0.0.1:16002")
		if err != nil {
			panic(err)
		}
	}()
	time.Sleep(100 * time.Millisecond)

	// create a request
	c, err := jsoffnet.NewClient("http://127.0.0.1:16002")
	assert.Nil(err)

	reqmsg := jsoff.NewRequestMessage(jsoff.NewUuid(), "say", []interface{}{"hi"})
	resmsg, err := c.Call(rootCtx, reqmsg)
	assert.Nil(err)
	assert.True(resmsg.IsResult())
	assert.Equal("echo hi", resmsg.MustResult())
}

func TestPlaybookEndpoint(t *testing.T) {
	assert := assert.New(t)

	rootCtx := context.Background()

	// start a normal jsonrpc Server
	server := jsoffnet.NewHttp1Handler(nil)
	server.Actor.OnTyped("say", func(req *jsoffnet.RPCRequest, a string) (string, error) {
		return "echo " + a, nil
	})
	go jsoffnet.ListenAndServe(rootCtx, "127.0.0.1:16004", server)

	// start rpcmux server
	actor := app.NewActor()
	var handler http.Handler
	handler = jsoffnet.NewGatewayHandler(rootCtx, actor, true)
	go jsoffnet.ListenAndServe(rootCtx, "127.0.0.1:16003", handler)
	time.Sleep(100 * time.Millisecond)

	// create playbook instance and run
	pb := NewPlaybook()
	err := pb.Config.LoadBytes([]byte(PbEndpoint))
	assert.Nil(err)

	method, ok := pb.Config.Methods["say"]
	assert.True(ok)
	assert.NotNil(method.innerSchema)
	assert.Equal("method", method.innerSchema.Type())

	go func() {
		err := pb.Run(rootCtx, "h2c://127.0.0.1:16003")
		if err != nil {
			panic(err)
		}
	}()
	time.Sleep(100 * time.Millisecond)

	// create a request
	c, err := jsoffnet.NewClient("http://127.0.0.1:16003")
	assert.Nil(err)

	reqmsg := jsoff.NewRequestMessage(jsoff.NewUuid(), "say", []interface{}{"hi"})
	resmsg, err := c.Call(rootCtx, reqmsg)
	assert.Nil(err)
	assert.True(resmsg.IsResult())
	assert.Equal("echo hi", resmsg.MustResult())
}
