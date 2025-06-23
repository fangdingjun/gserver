package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fangdingjun/go-log"
	"golang.org/x/net/trace"
)

// handler process the proxy request first(if enabled)
// and route the request to the registered http.Handler
type handler struct {
	handler http.Handler
	cfg     *conf
	events  trace.EventLog
}

var defaultTransport http.RoundTripper = &http.Transport{
	//DialContext:         dialContext,
	MaxIdleConns:          50,
	IdleConnTimeout:       30 * time.Second,
	MaxIdleConnsPerHost:   3,
	DisableKeepAlives:     true,
	ResponseHeaderTimeout: 2 * time.Second,
}
var defaultDialer = &net.Dialer{Timeout: 3 * time.Second}

type proxyDialer struct {
	u *url.URL
}

func (p *proxyDialer) Dial(r *http.Request) (net.Conn, error) {
	conn, err := defaultDialer.Dial("tcp", p.u.Host)
	if err != nil {
		log.Errorln(err)
		return nil, err
	}
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\n\r\n", r.RequestURI)
	tr := textproto.NewReader(bufio.NewReader(conn))
	codeline, err := tr.ReadLine()
	if err != nil {
		conn.Close()
		return nil, err
	}
	ss := strings.SplitN(codeline, " ", 3)
	code, err := strconv.Atoi(ss[1])
	if err != nil || code != 200 {
		conn.Close()
		return nil, fmt.Errorf("dial failed")
	}
	_, err = tr.ReadMIMEHeader()
	if err != nil {
		log.Errorln(err)
		conn.Close()
		return nil, err
	}

	return conn, nil
}

var defaultProxyDial *proxyDialer

var needProxyDomains map[string]int
var proxyDomainMu sync.Mutex

func needProxy(r *http.Request) bool {
	host, _, _ := net.SplitHostPort(r.RequestURI)

	proxyDomainMu.Lock()
	defer proxyDomainMu.Unlock()

	for k := range needProxyDomains {
		if strings.HasSuffix(host, k) {
			log.Debugf("%s through proxy", host)
			return true
		}
	}
	return false
}

func initProxy(c *conf) {
	if c.ProxyUp != "" {
		u, err := url.Parse(c.ProxyUp)
		if err != nil {
			log.Errorln(err)
			return
		}
		log.Infof("proxy %s", u.Host)
		defaultProxyDial = &proxyDialer{u: u}
	}
}

type logWriter struct {
	mu sync.Mutex
	fp *os.File
}

func (l *logWriter) log(s string) error {
	if l.fp == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.fp, "%s\n", s)
	return nil
}

func (l *logWriter) Close() error {
	l.fp.Close()
	return nil
}

var failLog *logWriter

func init() {
	failLog = &logWriter{}
	fp, err := os.OpenFile("failed_domains.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Errorln(err)
		return
	}
	failLog.fp = fp
}

// ServeHTTP implements the http.Handler interface
func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// http/1.1 local request
	if r.ProtoMajor == 1 && r.RequestURI[0] == '/' {
		h.events.Printf("http11 local request %s", r.URL.Path)
		if h.handler != nil {
			h.handler.ServeHTTP(w, r)
		} else {
			http.DefaultServeMux.ServeHTTP(w, r)
		}
		return
	}

	// http/2.0 local request
	if r.ProtoMajor == 2 && h.isLocalRequest(r) {
		h.events.Printf("http2 local request %s", r.URL.Path)
		if h.handler != nil {
			h.handler.ServeHTTP(w, r)
		} else {
			http.DefaultServeMux.ServeHTTP(w, r)
		}
		return
	}

	// proxy request

	if r.ProtoMajor == 1 && !h.cfg.Proxy.HTTP1Proxy {
		h.events.Errorf("http1.1 request not exists path %s", r.URL.Path)
		http.Error(w, "<h1>404 Not Found</h1>", http.StatusNotFound)
		return
	}

	if r.ProtoMajor == 2 && !h.cfg.Proxy.HTTP2Proxy {
		h.events.Errorf("http2 request not exists path %s", r.URL.Path)
		http.Error(w, "<h1>404 Not Found</h1>", http.StatusNotFound)
		return
	}

	if r.Method == http.MethodConnect {
		// CONNECT request
		h.handleCONNECT(w, r)
	} else {
		// GET, POST, PUT, ....
		h.handleHTTP(w, r)
	}
}

func (h *handler) handleHTTP(w http.ResponseWriter, r *http.Request) {

	var resp *http.Response
	var err error

	log.Infof("%s %s", r.Method, r.RequestURI)

	r.Header.Del("proxy-connection")
	r.Header.Del("proxy-authorization")

	if r.ProtoMajor == 2 {
		r.URL.Scheme = "http"
		r.URL.Host = r.Host
		r.RequestURI = r.URL.String()
		if r.Method != http.MethodPost && r.Method != http.MethodPut {
			r.ContentLength = 0
			r.Body.Close()
			r.Body = nil
		}
	}

	h.events.Printf("%s proxy request %s", r.Proto, r.RequestURI)

	resp, err = defaultTransport.RoundTrip(r)
	if err != nil {
		h.events.Errorf("roundtrip %s, error %s", r.RequestURI, err)
		log.Errorf("RoundTrip %s: %s", r.RequestURI, err)
		failLog.log(r.RequestURI)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	defer resp.Body.Close()

	hdr := w.Header()

	resp.Header.Del("connection")

	for k, v := range resp.Header {
		for _, v1 := range v {
			hdr.Add(k, v1)
		}
	}

	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

type flushWriter struct {
	w io.Writer
}

func (fw flushWriter) Write(buf []byte) (int, error) {
	n, err := fw.w.Write(buf)
	fw.w.(http.Flusher).Flush()
	return n, err
}

func (h *handler) handleCONNECT(w http.ResponseWriter, r *http.Request) {
	host := r.RequestURI

	log.Infof("%s %s", r.Method, r.RequestURI)

	if r.ProtoMajor == 2 {
		host = r.URL.Host
	}
	h.events.Printf("proxy request %s %s", r.Method, host)
	if !strings.Contains(host, ":") {
		host = fmt.Sprintf("%s:443", host)
	}

	var conn net.Conn
	var err error

	if needProxy(r) && defaultProxyDial != nil {
		conn, err = defaultProxyDial.Dial(r)
		if err != nil {
			log.Errorln(err)
			http.Error(w, "", http.StatusServiceUnavailable)
			return
		}
	} else {
		conn, err = defaultDialer.Dial("tcp", host)
		if err != nil {
			h.events.Errorf("dial %s, error %s", host, err)
			log.Errorf("net.dial %s: %s", host, err)
			msg := fmt.Sprintf("dial to %s failed: %s", host, err)
			failLog.log(host)
			http.Error(w, msg, http.StatusServiceUnavailable)
			return
		}
	}

	if r.ProtoMajor == 1 {
		// HTTP/1.1
		hj, _ := w.(http.Hijacker)
		conn1, _, _ := hj.Hijack()

		fmt.Fprintf(conn1, "%s 200 connection established\r\n\r\n", r.Proto)

		pipeAndClose(conn, conn1)
		return
	}

	// HTTP/2.0

	defer func() {
		if err := recover(); err != nil {
			h.events.Errorf("http2 data pipe, panic %s", err)
			log.Errorf("recover %+v", err)
		}
	}()

	defer conn.Close()

	w.WriteHeader(http.StatusOK)
	w.(http.Flusher).Flush()

	//h.events.Printf("data forward")
	ch := make(chan struct{}, 2)
	go func() {
		io.Copy(conn, r.Body)
		ch <- struct{}{}
	}()

	go func() {
		io.Copy(flushWriter{w}, conn)
		ch <- struct{}{}
	}()

	<-ch
}

// isLocalRequest determine the http2 request is local path request
// or the proxy request
func (h *handler) isLocalRequest(r *http.Request) bool {
	if !h.cfg.Proxy.HTTP2Proxy {
		return true
	}

	if len(h.cfg.Proxy.LocalDomains) == 0 {
		return true
	}

	host := r.Host
	if h1, _, err := net.SplitHostPort(r.Host); err == nil {
		host = h1
	}

	for _, s := range h.cfg.Proxy.LocalDomains {
		if strings.HasSuffix(host, s) {
			return true
		}
	}

	return false
}

func pipeAndClose(r1, r2 io.ReadWriteCloser) {
	tr := trace.New("proxy", "data pipe")
	defer tr.Finish()

	defer func() {
		if err := recover(); err != nil {
			log.Errorf("recover %+v", err)
			tr.LazyPrintf("http 1.1 data pipe, recover %+v", err)
			tr.SetError()
		}
	}()

	defer r1.Close()
	defer r2.Close()

	ch := make(chan struct{}, 2)
	go func() {
		io.Copy(r1, r2)
		ch <- struct{}{}
	}()

	go func() {
		io.Copy(r2, r1)
		ch <- struct{}{}
	}()

	<-ch
}
