package playbook

import (
	"context"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/superisaac/jsonz"
	//"github.com/superisaac/jsonz/schema"
	"encoding/json"
	"github.com/superisaac/rpcmap/worker"
	"io"
	"os"
	"os/exec"
	"time"
)

func NewPlaybook() *Playbook {
	return &Playbook{}
}

func (self MethodT) CanExec() bool {
	return self.Shell != nil && self.Shell.Cmd != ""
}

func (self MethodT) Exec(req *rpcmapworker.WorkerRequest, methodName string) (interface{}, error) {
	msg := req.Msg
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

	msgJson := jsonz.MessageString(msg)
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

func (self *Playbook) Run(rootCtx context.Context, serverAddress string) error {
	worker := rpcmapworker.NewServiceWorker([]string{serverAddress})

	for name, method := range self.Config.Methods {
		if !method.CanExec() {
			log.Warnf("cannot exec method %s %+v %s\n", name, method, method.Shell.Cmd)
			continue
		}
		log.Infof("playbook register %s", name)
		opts := make([]rpcmapworker.WorkerHandlerSetter, 0)
		if method.innerSchema != nil {
			opts = append(opts, rpcmapworker.WithSchema(method.innerSchema))
		}

		worker.On(name, func(req *rpcmapworker.WorkerRequest, params []interface{}) (interface{}, error) {
			req.Msg.Log().Infof("begin exec %s", name)
			v, err := method.Exec(req, name)
			if err != nil {
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) {
					req.Msg.Log().Warnf(
						"command exit, code: %d, stderr: %s",
						exitErr.ExitCode(),
						string(exitErr.Stderr)[:100])
					return nil, jsonz.ErrLiveExit
				}

				req.Msg.Log().Warnf("error exec %s, %s", name, err.Error())
			} else {
				req.Msg.Log().Infof("end exec %s", name)
			}
			return v, err
		}, opts...)
	}

	worker.ConnectWait(rootCtx)
	return nil
}
