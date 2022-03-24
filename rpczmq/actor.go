package rpczmq

import (
	"context"
	log "github.com/sirupsen/logrus"
	"github.com/superisaac/jsonz"
	"github.com/superisaac/jsonz/http"
	"net/http"
	"sync"
)

const (
	tailSchema = `
---
type: method
description: get the tail elements
params:
  - type: number
    name: count
    description: item count
`

	getSchema = `
---
type: method
description: get a range of mq 
params:
  - name: prevID
    type: string
    description: previous id, empty prevID means take the last item
  - name: count
    type: integer
    description: get count
`

	addSchema = `
---
type: method
description: rpczmq.add add a notify methods
params:
  - name: notifymethod
    type: string
additionalParams:
  type: any
`
	subscribeSchema = `
---
type: method
description: rpczmq.subscribe subscribe a stream of notify message
params: []
additionalParams:
  type: string
  name: method
`
)

func extractNamespace(ctx context.Context) string {
	if v := ctx.Value("authInfo"); v != nil {
		authInfo, _ := v.(*jsonzhttp.AuthInfo)
		if authInfo != nil && authInfo.Settings != nil {
			if nv, ok := authInfo.Settings["namespace"]; ok {
				if ns, ok := nv.(string); ok {
					return ns
				}
			}

		}

	}
	return "default"
}

type subscription struct {
	subID      string
	context    context.Context
	cancelFunc func()
}

func NewActor(mqurl string) *jsonzhttp.Actor {
	actor := jsonzhttp.NewActor()

	subscriptions := sync.Map{}

	// currently only support redis
	log.Infof("create mq actor, currently only redis mq is supported")

	mqclient := NewMQClient(mqurl)

	actor.OnTyped("rpczmq.get", func(req *jsonzhttp.RPCRequest, prevID string, count int) (map[string]interface{}, error) {
		ns := extractNamespace(req.HttpRequest().Context())
		chunk, err := mqclient.Chunk(
			req.Context(),
			ns, prevID, int64(count))
		if err != nil {
			return nil, err
		}
		return chunk.JsonResult(), err
	}, jsonzhttp.WithSchemaYaml(getSchema))

	actor.OnTyped("rpczmq.tail", func(req *jsonzhttp.RPCRequest, count int) (map[string]interface{}, error) {
		ns := extractNamespace(req.HttpRequest().Context())
		chunk, err := mqclient.Tail(
			req.Context(),
			ns, int64(count))
		if err != nil {
			return nil, err
		}
		return chunk.JsonResult(), err
	}, jsonzhttp.WithSchemaYaml(tailSchema))

	actor.On("rpczmq.add", func(req *jsonzhttp.RPCRequest, params []interface{}) (interface{}, error) {
		if len(params) == 0 {
			return nil, jsonz.ParamsError("notify method not provided")
		}
		ns := extractNamespace(req.HttpRequest().Context())

		method, ok := params[0].(string)
		if !ok {
			return nil, jsonz.ParamsError("method is not string")
		}

		ntf := jsonz.NewNotifyMessage(method, params[1:])
		id, err := mqclient.Add(req.Context(), ns, ntf)
		return id, err
	}, jsonzhttp.WithSchemaYaml(addSchema))

	actor.On("rpczmq.subscribe", func(req *jsonzhttp.RPCRequest, params []interface{}) (interface{}, error) {
		session := req.Session()
		if session == nil {
			return nil, jsonz.ErrMethodNotFound
		}
		if _, ok := subscriptions.Load(session.SessionID()); ok {
			// this session already subscribed
			log.Warnf("rpczmq.subscribe already called on session %s", session.SessionID())
			return nil, jsonz.ErrMethodNotFound
		}
		ns := extractNamespace(req.HttpRequest().Context())
		var mths []string
		err := jsonz.DecodeInterface(params, &mths)
		if err != nil {
			log.Warnf("decode methods %s", err)
			return nil, err
		}
		methods := map[string]bool{}

		for _, method := range mths {
			methods[method] = true
		}

		ctx, cancel := context.WithCancel(req.HttpRequest().Context())
		sub := &subscription{
			subID:      jsonz.NewUuid(),
			context:    ctx,
			cancelFunc: cancel,
		}
		subscriptions.Store(session.SessionID(), sub)
		log.Infof("subscription %s created", sub.subID)

		go func() {
			err := mqclient.Subscribe(ctx, ns, func(item MQItem) {
				if _, ok := methods[item.Brief]; !ok {
					return
				}
				ntf := item.Notify()
				ntfmap, err := jsonz.MessageMap(ntf)
				if err != nil {
					panic(err)
				}
				params := map[string]interface{}{
					"subscription": sub.subID,
					"mqID":         item.ID,
					"msg":          ntfmap,
				}
				itemntf := jsonz.NewNotifyMessage("rpcz.item", params)
				session.Send(itemntf)
			})
			if err != nil {
				log.Errorf("subscribe error %s", err)
			}
		}()
		return sub.subID, nil
	}, jsonzhttp.WithSchemaYaml(subscribeSchema)) // end of on rpczmq.subscribe

	actor.OnClose(func(r *http.Request, session jsonzhttp.RPCSession) {
		if v, ok := subscriptions.LoadAndDelete(session.SessionID()); ok {
			sub, _ := v.(*subscription)
			log.Infof("cancel subscription %s", sub.subID)
			sub.cancelFunc()
		}
	})
	return actor
}