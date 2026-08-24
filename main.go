// OpenAI 兼容透明反向代理 —— 单二进制零依赖(stdlib)。
// 客户端 PROXY_API_KEY 认证, 上游 UPSTREAM_API_KEY/UPSTREAM_BASE_URL 转发。
// /v1/* 透传(含 SSE 流式), 剥离 hop-by-hop/source 头。
package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const JSON = "application/json"

var (
	proxyKey     string
	upstreamKey  string
	upstreamBase *url.URL
	models       = []string{"gpt-5.6-luna", "gpt-5.6-sol", "gpt-5.6-terra"}
)

var hopByHop = map[string]bool{
	"connection": true, "keep-alive": true, "proxy-authenticate": true,
	"proxy-authorization": true, "te": true, "trailer": true,
	"transfer-encoding": true, "upgrade": true, "host": true, "content-length": true,
}

// 剥离客户端注入/代理相关头, 再换上鉴权头
func filteredHeaders(h http.Header) http.Header {
	strip := map[string]bool{
		"authorization": true, "cf-ray": true, "cf-connecting-ip": true,
		"cf-ipcountry": true, "cdn-loop": true, "x-forwarded-for": true,
		"x-forwarded-host": true, "x-forwarded-port": true, "x-forwarded-proto": true,
		"x-real-ip": true, "x-request-id": true, "accept-encoding": true, "expect": true,
	}
	out := make(http.Header)
	for k, vs := range h {
		lk := strings.ToLower(k)
		if strip[lk] || strings.HasPrefix(lk, "sec-") {
			continue
		}
		for _, v := range vs {
			out.Add(k, v)
		}
	}
	out.Set("Authorization", "Bearer "+upstreamKey)
	return out
}

func jsonErr(w http.ResponseWriter, status int, msg, typ string) {
	w.Header().Set("Content-Type", JSON)
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"error":{"message":%q,"type":%q}}`, msg, typ)
}

func authenticate(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+proxyKey {
			jsonErr(w, http.StatusUnauthorized, "Unauthorized", "authentication_error")
			return
		}
		next(w, r)
	}
}

func proxy(w http.ResponseWriter, r *http.Request) {
	target := *upstreamBase
	target.Path = strings.TrimPrefix(r.URL.Path, "/v1/")
	target.RawQuery = r.URL.RawQuery

	req, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), r.Body)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "Invalid request path", "invalid_request_error")
		return
	}
	req.Header = filteredHeaders(r.Header)

	client := &http.Client{
		Transport: &http.Transport{ResponseHeaderTimeout: 300e9}, // 300s, SSE/长生成
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		jsonErr(w, http.StatusBadGateway, "Upstream request failed", "proxy_error")
		return
	}
	defer resp.Body.Close()

	for k, vs := range resp.Header {
		if hopByHop[strings.ToLower(k)] {
			continue
		}
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			w.Write(buf[:n])
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr != nil {
			break
		}
	}
}

func main() {
	proxyKey = os.Getenv("PROXY_API_KEY")
	if proxyKey == "" {
		log.Fatal("PROXY_API_KEY is required")
	}
	base := os.Getenv("UPSTREAM_BASE_URL")
	upstreamKey = os.Getenv("UPSTREAM_API_KEY")
	if base == "" || upstreamKey == "" {
		log.Fatal("UPSTREAM_BASE_URL and UPSTREAM_API_KEY are required")
	}
	var err error
	upstreamBase, err = url.Parse(strings.TrimSuffix(base, "/") + "/")
	if err != nil {
		log.Fatal("Invalid UPSTREAM_BASE_URL")
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", JSON)
		fmt.Fprint(w, `{"status":"ok"}`)
	})
	mux.HandleFunc("/v1/models", authenticate(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", JSON)
		fmt.Fprint(w, `{"object":"list","data":[`)
		for i, m := range models {
			if i > 0 {
				fmt.Fprint(w, ",")
			}
			fmt.Fprintf(w, `{"id":%q,"object":"model","created":0,"owned_by":"proxy"}`, m)
		}
		fmt.Fprint(w, `]}`)
	}))
	mux.HandleFunc("/v1/", authenticate(proxy))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		jsonErr(w, http.StatusNotFound, "Not found", "not_found_error")
	})

	log.Printf("OpenAI proxy listening on port %s", port)
	log.Fatal(http.ListenAndServe("0.0.0.0:"+port, mux))
}