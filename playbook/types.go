package playbook

import (
	"github.com/superisaac/jlib/http"
	"github.com/superisaac/jlib/schema"
)

type ShellT struct {
	Cmd     string   `yaml:"command"`
	Env     []string `yaml:"env,omitempty"`
	Timeout *int     `yaml:"timeout,omitempty"`
}

type EndpointConfig struct {
	Urlstr  string            `yaml:"url"`
	Header  map[string]string `yaml:"header"`
	Timeout *int              `yaml:"timeout,omitempty"`

	client jlibhttp.Client `yaml:"-"`
}

type MethodConfig struct {
	Description     string            `yaml:"description,omitempty"`
	SchemaInterface interface{}       `yaml:"schema,omitempty"`
	Shell           *ShellT           `yaml:"shell,omitempty"`
	Endpoint        *EndpointConfig   `yaml:"api,omitempty"`
	innerSchema     jlibschema.Schema `yaml:"-"`
}

type PlaybookConfig struct {
	Version string                     `yaml:"version,omitempty"`
	Methods map[string](*MethodConfig) `yaml:"methods,omitempty"`
}

type PlaybookOptions struct {
	Concurrency int
}

type Playbook struct {
	Config  PlaybookConfig
	Options PlaybookOptions
}
