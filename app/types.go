package app

import (
	"context"
	"github.com/superisaac/jsoff"
	"github.com/superisaac/jsoff/net"
	"github.com/superisaac/jsoff/schema"
	"github.com/superisaac/rpcmux/mq"
	"net/url"
	"sync"
	"time"
)

type MQConfig struct {
	Urlstr string   `yaml:"url"`
	url    *url.URL `yaml:"-"`
}

type AppConfig struct {
	Server struct {
		Bind         string               `yaml:"bind"`
		AdvertiseUrl string               `yaml:"advertise_url,omitempty"`
		Auth         *jsoffnet.AuthConfig `yaml:"auth,omitempty"`
		TLS          *jsoffnet.TLSConfig  `yaml:"tls,omitempty"`
	} `yaml:"server"`

	MQ MQConfig `yaml:"mq,omitempty"`
}

type App struct {
	routers    sync.Map
	Config     *AppConfig
	ctx        context.Context
	cancelFunc func()
}

// router related
type pendingT struct {
	orig          *jsoff.RequestMessage
	resultChannel chan jsoff.Message
	toService     *Service
	expiration    time.Time
}

type serviceStatus struct {
	AdvertiseUrl string   `json:"advertise_url"`
	Methods      []string `json:"methods"`
	Timestamp    int64    `json:"timestamp"`
}

type RemoteService struct {
	AdvertiseUrl string
	Methods      map[string]bool
	UpdateAt     time.Time

	client jsoffnet.Client
}

type Router struct {
	namespace string
	mqSection string

	app *App

	// context
	ctx        context.Context
	cancelFunc func()

	// start
	startOnce sync.Once

	// service indices
	serviceIndex        sync.Map
	serviceLock         sync.RWMutex
	methodServicesIndex map[string][]*Service

	// remote service indices
	remoteServiceIndex   sync.Map
	remoteServiceLock    sync.RWMutex
	methodRemoteServices map[string][]*RemoteService

	// pending requests
	pendings sync.Map

	// mq
	mqClient mq.MQClient
}

type Service struct {
	router  *Router
	session jsoffnet.RPCSession
	methods map[string]jsoffschema.Schema
}
