package main

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"crypto/tls"
	"flag"
	"fmt"
	"context"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"golang.org/x/time/rate"
)

func init() {
	flag.Usage = func() {
		h := []string{
			"Safe URL Fetcher for Bug Bounty Hunting",
			"",
			"Options:",
			"  -b, --body <data>         Request body",
			"  -d, --delay <delay>       Delay between issuing requests (ms)",
			"  -H, --header <header>     Add a header to the request (can be specified multiple times)",
			"      --ignore-html         Don't save HTML files; useful when looking for non-HTML files only",
			"      --ignore-empty        Don't save empty files",
			"  -k, --keep-alive          Use HTTP Keep-Alive",
			"  -m, --method              HTTP method to use (default: GET, or POST if body is specified)",
			"  -M, --match <string>      Save responses that include <string> in the body",
			"  -o, --output <dir>        Directory to save responses in (will be created)",
			"  -s, --save-status <code>  Save responses with given status code (can be specified multiple times)",
			"  -S, --save                Save all responses",
			"  -x, --proxy <proxyURL>    Use the provided HTTP proxy",
			"",
		}

		fmt.Fprintf(os.Stderr, strings.Join(h, "\n"))
	}
}

func main() {

	var requestBody string
	flag.StringVar(&requestBody, "body", "", "")
	flag.StringVar(&requestBody, "b", "", "")

	var keepAlives bool
	flag.BoolVar(&keepAlives, "keep-alive", false, "")
	flag.BoolVar(&keepAlives, "keep-alives", false, "")
	flag.BoolVar(&keepAlives, "k", false, "")

	var saveResponses bool
	flag.BoolVar(&saveResponses, "save", false, "")
	flag.BoolVar(&saveResponses, "S", false, "")

	var delayMs int
	flag.IntVar(&delayMs, "delay", 500, "")
	flag.IntVar(&delayMs, "d", 500, "")

	var method string
	flag.StringVar(&method, "method", "GET", "")
	flag.StringVar(&method, "m", "GET", "")

	var match string
	flag.StringVar(&match, "match", "", "")
	flag.StringVar(&match, "M", "", "")

	var outputDir string
	flag.StringVar(&outputDir, "output", "out", "")
	flag.StringVar(&outputDir, "o", "out", "")

	var headers headerArgs
	flag.Var(&headers, "header", "")
	flag.Var(&headers, "H", "")

	var saveStatus saveStatusArgs
	flag.Var(&saveStatus, "save-status", "")
	flag.Var(&saveStatus, "s", "")

	var proxy string
	flag.StringVar(&proxy, "proxy", "", "")
	flag.StringVar(&proxy, "x", "", "")

	var ignoreHTMLFiles bool
	flag.BoolVar(&ignoreHTMLFiles, "ignore-html", false, "")

	var ignoreEmpty bool
	flag.BoolVar(&ignoreEmpty, "ignore-empty", false, "")

	flag.Parse()

	delay := time.Duration(delayMs) * time.Millisecond
	client := newClient(keepAlives, proxy)
	prefix := outputDir

	isHTML := regexp.MustCompile(`(?i)<html`)
	limiter := rate.NewLimiter(rate.Every(delay), 1)

	var wg sync.WaitGroup
	sc := bufio.NewScanner(os.Stdin)

	for sc.Scan() {
		rawURL := sc.Text()
		wg.Add(1)

		go func(rawURL string) {
			defer wg.Done()

			err := limiter.Wait(context.Background())
			if err != nil {
				fmt.Fprintf(os.Stderr, "rate limiter error: %s\n", err)
				return
			}

			var b io.Reader
			if requestBody != "" {
				b = strings.NewReader(requestBody)
				if method == "GET" {
					method = "POST"
				}
			}

			_, err = url.ParseRequestURI(rawURL)
			if err != nil {
				fmt.Fprintf(os.Stderr, "invalid URL: %s\n", rawURL)
				return
			}

			req, err := http.NewRequest(method, rawURL, b)
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to create request: %s\n", err)
				return
			}

			for _, h := range headers {
				parts := strings.SplitN(h, ":", 2)
				if len(parts) != 2 {
					continue
				}
				req.Header.Set(parts[0], strings.TrimSpace(parts[1]))
			}

			resp, err := client.Do(req)
			if err != nil {
				fmt.Fprintf(os.Stderr, "request failed: %s\n", err)
				return
			}
			defer resp.Body.Close()

			responseBody, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to read body: %s\n", err)
				return
			}

			shouldSave := saveResponses || saveStatus.Includes(resp.StatusCode)

			if ignoreHTMLFiles {
				shouldSave = shouldSave && !isHTML.Match(responseBody)
			}

			if ignoreEmpty {
				shouldSave = shouldSave && len(bytes.TrimSpace(responseBody)) != 0
			}

			if match != "" && bytes.Contains(responseBody, []byte(match)) {
				shouldSave = true
			}

			if !shouldSave {
				fmt.Printf("%s %d\n", rawURL, resp.StatusCode)
				return
			}

			normalisedPath := normalisePath(req.URL)
			hash := sha1.Sum([]byte(method + rawURL + requestBody + headers.String()))
			p := path.Join(prefix, req.URL.Hostname(), normalisedPath, fmt.Sprintf("%x.body", hash))
			err = os.MkdirAll(path.Dir(p), 0750)
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to create dir: %s\n", err)
				return
			}

			err = ioutil.WriteFile(p, responseBody, 0644)
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to write file contents: %s\n", err)
				return
			}

			headersPath := path.Join(prefix, req.URL.Hostname(), normalisedPath, fmt.Sprintf("%x.headers", hash))
			headersFile, err := os.Create(headersPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to create file: %s\n", err)
				return
			}
			defer headersFile.Close()

			var buf strings.Builder
			buf.WriteString(fmt.Sprintf("%s %s\n\n", method, rawURL))
			for _, h := range headers {
				buf.WriteString(fmt.Sprintf("> %s\n", h))
			}
			buf.WriteRune('\n')

			if requestBody != "" {
				buf.WriteString(requestBody)
				buf.WriteString("\n\n")
			}

			buf.WriteString(fmt.Sprintf("< %s %s\n", resp.Proto, resp.Status))
			for k, vs := range resp.Header {
				for _, v := range vs {
					buf.WriteString(fmt.Sprintf("< %s: %s\n", k, v))
				}
			}

			_, err = io.Copy(headersFile, strings.NewReader(buf.String()))
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to write file contents: %s\n", err)
				return
			}

			fmt.Printf("%s: %s %d\n", p, rawURL, resp.StatusCode)
		}(rawURL)
	}

	wg.Wait()
}

func newClient(keepAlives bool, proxy string) *http.Client {
	tr := &http.Transport{
		MaxIdleConns:      30,
		IdleConnTimeout:   time.Second,
		DisableKeepAlives: !keepAlives,
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: false},
		DialContext: (&net.Dialer{
			Timeout:   time.Second * 10,
			KeepAlive: time.Second,
		}).DialContext,
	}

	if proxy != "" {
		if p, err := url.Parse(proxy); err == nil {
			tr.Proxy = http.ProxyURL(p)
		}
	}

	re := func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	return &http.Client{
		Transport:     tr,
		CheckRedirect: re,
		Timeout:       time.Second * 10,
	}
}

type headerArgs []string

func (h *headerArgs) Set(val string) error {
	*h = append(*h, val)
	return nil
}

func (h headerArgs) String() string {
	return strings.Join(h, ", ")
}

type saveStatusArgs []int

func (s *saveStatusArgs) Set(val string) error {
	i, _ := strconv.Atoi(val)
	*s = append(*s, i)
	return nil
}

func (s saveStatusArgs) String() string {
	return "string"
}

func (s saveStatusArgs) Includes(search int) bool {
	for _, status := range s {
		if status == search {
			return true
		}
	}
	return false
}

func normalisePath(u *url.URL) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9/._-]+`)
	return re.ReplaceAllString(u.Path, "-")
}
