package main

import (
	"html/template"
	"os"
	"testing"

	"github.com/fangdingjun/go-log"
	"github.com/stretchr/testify/require"
)

func TestTmpelOUt(t *testing.T) {
	var proxiedHosts = map[string]int{
		"a.com": 1,
		"b.com": 1,
	}

	var noProxiedHosts = map[string]int{
		"c.com": 1,
		"d.com": 1,
	}
	d1, err := os.ReadFile("proxy.tmpl")
	require.Nil(t, err)

	tmpl, err := template.New("o").Parse(string(d1))
	require.Nil(t, err)

	type aaa struct {
		ProxiedHosts   map[string]int
		NoProxiedHosts map[string]int
		Proxy          string
		Default        string
	}

	err = tmpl.Execute(os.Stdout, aaa{ProxiedHosts: proxiedHosts, NoProxiedHosts: noProxiedHosts, Proxy: "127.0.0.2:3333", Default: "127.0.0.1:112"})
	if err != nil {
		log.Infof("%s", err)
	}
	require.Nil(t, err)
}
