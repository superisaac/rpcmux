package rpczapp

import (
	"context"
	"fmt"
	"github.com/superisaac/jsonz"
	"github.com/superisaac/jsonz/http"
	"github.com/superisaac/jsonz/schema"
	"github.com/superisaac/rpcz/rpczmq"
	"net/http"
)

const (
	declareSchema = `
---
type: method
description: declare serve methods, only callable via stream requests
params:
  - anyOf:
    - type: object
      properties: {}
    - type: "null"
`
	showSchemaSchema = `
---
type: method
description: show the schema of a method
params:
  - type: string
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

func NewActor(cfg *RPCZConfig) *jsonzhttp.Actor {
	if cfg == nil {
		cfg = &RPCZConfig{}
	}

	actor := jsonzhttp.NewActor()
	children := []*jsonzhttp.Actor{}

	if cfg.MQUrl != "" {
		mqactor := rpczmq.NewActor(cfg.MQUrl)
		children = append(children, mqactor)
	}

	// declare methods
	actor.OnTyped("rpcz.declare", func(req *jsonzhttp.RPCRequest, methods map[string]interface{}) (string, error) {
		session := req.Session()
		if session == nil {
			return "", jsonz.ErrMethodNotFound
		}
		ns := extractNamespace(req.HttpRequest().Context())
		router := GetRouter(ns)
		service := router.GetService(session)

		methodSchemas := map[string]jsonzschema.Schema{}
		for mname, smap := range methods {
			if smap == nil {
				methodSchemas[mname] = nil
			} else {
				builder := jsonzschema.NewSchemaBuilder()
				s, err := builder.Build(smap)
				if err != nil {
					return "", jsonz.ParamsError(fmt.Sprintf("schema of %s build failed", mname))
				}
				methodSchemas[mname] = s
			}
		}
		err := service.UpdateMethods(methodSchemas)
		if err != nil {
			return "", err
		}
		return "ok", nil
	}, jsonzhttp.WithSchemaYaml(declareSchema))

	actor.OnTyped("rpcz.schema", func(req *jsonzhttp.RPCRequest, method string) (map[string]interface{}, error) {
		// from actor
		if actor.Has(method) {
			if schema, ok := actor.GetSchema(method); ok {
				return schema.RebuildType(), nil
			} else {
				return nil, jsonz.ParamsError("no schema")
			}
		}

		// from children
		for _, c := range children {
			if !c.Has(method) {
				continue
			}
			if schema, ok := c.GetSchema(method); ok {
				return schema.RebuildType(), nil
			} else {
				return nil, jsonz.ParamsError("no schema")
			}
		}

		// get schema from router
		ns := extractNamespace(req.HttpRequest().Context())
		router := GetRouter(ns)
		if srv, ok := router.SelectService(method); ok {
			if schema, ok := srv.GetSchema(method); ok {
				return schema.RebuildType(), nil
			} else {
				return nil, jsonz.ParamsError("no schema")
			}
		}
		return nil, jsonz.ParamsError("no schema")
	}, jsonzhttp.WithSchemaYaml(showSchemaSchema))

	actor.OnMissing(func(req *jsonzhttp.RPCRequest) (interface{}, error) {
		msg := req.Msg()
		if msg.IsRequestOrNotify() {
			for _, child := range children {
				if child.Has(msg.MustMethod()) {
					return child.Feed(req)
				}
			}
		}

		ns := extractNamespace(req.HttpRequest().Context())

		router := GetRouter(ns)
		return router.Feed(msg)
	})

	actor.OnClose(func(r *http.Request, session jsonzhttp.RPCSession) {
		ns := extractNamespace(r.Context())
		router := GetRouter(ns)
		if dismissed := router.DismissService(session.SessionID()); !dismissed {
			for _, child := range children {
				child.HandleClose(r, session)
			}
		}
	})
	return actor
}
