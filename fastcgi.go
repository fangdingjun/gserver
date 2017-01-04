package main

import (
	"fmt"
	"github.com/fangdingjun/gofast"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// FastCGI is a fastcgi client connection
type FastCGI struct {
	Network   string
	Addr      string
	DocRoot   string
	URLPrefix string
	//client  gofast.Client
}

// NewFastCGI creates a new FastCGI struct
func NewFastCGI(network, addr, docroot, urlPrefix string) (*FastCGI, error) {
	u := strings.TrimRight(urlPrefix, "/")
	return &FastCGI{network, addr, docroot, u}, nil
}

var fcgiPathInfo = regexp.MustCompile(`^(.*?\.php)(.*)$`)

// ServeHTTP implements http.Handler interface
func (f *FastCGI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.FastCGIPass(w, r)
}

// FastCGIPass pass the request to fastcgi socket
func (f *FastCGI) FastCGIPass(w http.ResponseWriter, r *http.Request) {
	// make sure server not access the file out of document root
	p1 := filepath.Clean(filepath.Join(f.DocRoot, r.URL.Path))
	p2 := filepath.Clean(f.DocRoot)
	if !strings.HasPrefix(p1, p2) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "invalid url")
		return
	}

	var scriptName, pathInfo, scriptFileName string

	conn, err := net.Dial(f.Network, f.Addr)
	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusBadGateway)
		return
	}

	defer conn.Close()

	client := gofast.NewClient(f.DocRoot, conn, 20)

	urlPath := r.URL.Path
	if f.URLPrefix != "" {
		urlPath = strings.Replace(r.URL.Path, f.URLPrefix, "", 1)
	}

	p := fcgiPathInfo.FindStringSubmatch(urlPath)

	if len(p) < 2 {
		if strings.HasSuffix(r.URL.Path, "/") {
			// redirect to index.php
			scriptName = filepath.Join(r.URL.Path, "index.php")
			pathInfo = ""
			scriptFileName = filepath.Join(f.DocRoot, urlPath, "index.php")
		} else {
			// serve static file in php directory
			fn := filepath.Join(f.DocRoot, urlPath)
			http.ServeFile(w, r, fn)
			return
		}
	} else {
		scriptName = p[1]
		pathInfo = p[2]
		scriptFileName = filepath.Join(f.DocRoot, scriptName)
	}

	req := client.NewRequest(r)

	// set ourself path, prefix stripped
	// req.Params["DOCUMENT_URI"] = scriptName
	req.Params["SCRIPT_NAME"] = scriptName
	req.Params["PHP_SELF"] = scriptName
	req.Params["PATH_INFO"] = pathInfo
	req.Params["SCRIPT_FILENAME"] = scriptFileName

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}

	req.Params["REQUEST_SCHEME"] = scheme

	resp, err := client.Do(req)
	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusBadGateway)
		return
	}

	err = resp.WriteTo(w, os.Stderr)
	if err != nil {
		log.Println(err)
	}

	resp.Close()
}
