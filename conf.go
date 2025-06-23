package main

import (
	"io/ioutil"

	"github.com/go-yaml/yaml"
)

type conf struct {
	Listens []listen `yaml:"listen"`
	Vhosts  []vhost  `yaml:"vhost"`
	Proxy   proxycfg `yaml:"proxy"`
	ProxyUp string   `yaml:"proxy_upstream"`
	Domains string   `yaml:"domains"`
}

type proxycfg struct {
	HTTP1Proxy   bool     `yaml:"http1-proxy"`
	HTTP2Proxy   bool     `yaml:"http2-proxy"`
	LocalDomains []string `yaml:"localdomains"`
}

type listen struct {
	Addr         string        `yaml:"addr"`
	Port         int16         `yaml:"port"`
	Certificates []certificate `yaml:"certificates"`
}

type certificate struct {
	CertFile string `yaml:"certfile"`
	KeyFile  string `yaml:"keyfile"`
}

type vhost struct {
	Docroot   string `yaml:"docroot"`
	Hostname  string `yaml:"hostname"`
	ProxyPass string `yaml:"proxypass"`
}

func loadConfig(fn string) (*conf, error) {
	data, err := ioutil.ReadFile(fn)
	if err != nil {
		return nil, err
	}

	var c conf
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, err
	}

	return &c, nil
}
