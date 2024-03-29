package playbook

import (
	"context"
	"encoding/json"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/superisaac/jsoff"
	"github.com/superisaac/jsoff/net"
	"github.com/superisaac/rpcmux/worker"
	"io"
	"net/http"
	"os"
	"os/exec"
	"time"
)

func NewPlaybook() *Playbook {
	return &Playbook{}
}

func (self MethodConfig) CanExecute() bool {
	return self.CanExecuteShell() || self.CanCallEndpoint()
}

func (self MethodConfig) CanExecuteShell() bool {
	return self.Shell != nil && self.Shell.Cmd != ""
}

func (self MethodConfig) CanCallEndpoint() bool {
	return self.Endpoint != nil && self.Endpoint.Urlstr != ""
}

func (self MethodConfig) ExecuteShell(req *jsoffnet.RPCRequest, methodName string) (interface{}, error) {
	msg := req.Msg()
	var ctx context.Context
	var cancel func()
	if self.Shell.Timeout != nil {
		ctx, cancel = context.WithTimeout(
			context.Background(),
			time.Second*time.Duration(*self.Shell.Timeout))
		defer cancel()
	} else {
		ctx, cancel = context.WithCancel(context.Background())
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", self.Shell.Cmd)

	cmd.Env = append(os.Environ(), self.Shell.Env...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	defer stdin.Close()

	msgJson := jsoff.MessageString(msg)
	io.WriteString(stdin, msgJson)
	stdin.Close()

	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	if cmd.Process != nil {
		msg.Log().Infof("command for %s received output, pid %#v", methodName, cmd.Process.Pid)
	}
	var parsed interface{}
	err = json.Unmarshal(out, &parsed)
	if err != nil {
		return nil, err
	}
	return parsed, nil
}

func (self MethodConfig) CallEndpoint(req *jsoffnet.RPCRequest, methodName string) (interface{}, error) {
	ep := self.Endpoint
	// ep is not nil
	if ep.client == nil {
		client, err := jsoffnet.NewClient(ep.Urlstr)
		if err != nil {
			return nil, err
		}
		if ep.Header != nil && len(ep.Header) > 0 {
			h := http.Header{}
			for k, v := range ep.Header {
				h.Add(k, v)
			}
			client.SetExtraHeader(h)
		}
		ep.client = client
	}

	var ctx context.Context
	var cancel func()

	if ep.Timeout != nil {
		ctx, cancel = context.WithTimeout(
			context.Background(),
			time.Second*time.Duration(*ep.Timeout))
		defer cancel()
	} else {
		ctx, cancel = context.WithCancel(context.Background())
		defer cancel()
	}

	msg := req.Msg()
	if msg.IsRequest() {
		reqmsg, _ := msg.(*jsoff.RequestMessage)
		resmsg, err := ep.client.Call(ctx, reqmsg)
		return resmsg, err
	} else {
		msg.Log().Infof("send to url %s", ep.Urlstr)
		err := ep.client.Send(ctx, msg)
		return nil, err
	}
}

func (self *Playbook) Run(rootCtx context.Context, serverAddress string) error {
	if self.Options.Concurrency <= 0 {
		self.Options.Concurrency = 1
	}
	serverUrls := make([]string, self.Options.Concurrency)
	for i := 0; i < self.Options.Concurrency; i++ {
		serverUrls[i] = serverAddress
	}
	w := worker.NewServiceWorker(serverUrls)

	for name, method := range self.Config.Methods {
		if !method.CanExecute() {
			log.Warnf("cannot exec method %s %#v", name, method)
			continue
		}
		log.Infof("playbook register %s", name)
		opts := make([]jsoffnet.HandlerSetter, 0)
		if method.innerSchema != nil {
			opts = append(opts, jsoffnet.WithSchema(method.innerSchema))
		}

		err := w.Actor.OnRequest(name, func(req *jsoffnet.RPCRequest, params []interface{}) (interface{}, error) {
			req.Msg().Log().Infof("begin exec %s", name)
			var v interface{}
			var err error
			if method.CanExecuteShell() {
				v, err = method.ExecuteShell(req, name)
			} else {
				v, err = method.CallEndpoint(req, name)
			}
			if err != nil {
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) {
					req.Msg().Log().Warnf(
						"command exit, code: %d, stderr: %s",
						exitErr.ExitCode(),
						string(exitErr.Stderr)[:100])
					return nil, jsoff.ErrLiveExit
				}

				req.Msg().Log().Warnf("error exec %s, %s", name, err.Error())
			} else {
				req.Msg().Log().Infof("end exec %s", name)
			}
			return v, err
		}, opts...)
		if err != nil {
			return err
		}
	}

	w.ConnectWait(rootCtx)
	return nil
}
