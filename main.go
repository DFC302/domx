package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"

	"golang.org/x/net/publicsuffix"
)

// Matches bare domains and full URLs (http/https). Lines that don't match are
// silently skipped, which lets this tool handle both clean domain lists and
// noisy files containing mixed content.
var lineRegex = regexp.MustCompile(
	`^(?:https?://)?[-a-zA-Z0-9@:%._+~#=]{1,256}\.[a-zA-Z0-9()]{1,6}(?:[-a-zA-Z0-9()@:%_+.~#?&/=]*)?$`,
)

type domainInfo struct {
	Input     string `json:"input"`
	TLD       string `json:"tld"`
	Domain    string `json:"domain"`
	Subdomain string `json:"subdomain"`
	Apex      string `json:"apex"`
}

func stripToHost(raw string) string {
	s := raw
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	if i := strings.IndexByte(s, '/'); i != -1 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, '?'); i != -1 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, '#'); i != -1 {
		s = s[:i]
	}
	// Strip userinfo (user:pass@host or user@host).
	if i := strings.LastIndexByte(s, '@'); i != -1 {
		s = s[i+1:]
	}
	// Strip port only when the suffix after the last colon is all digits.
	if i := strings.LastIndexByte(s, ':'); i != -1 {
		suffix := s[i+1:]
		isPort := len(suffix) > 0
		for _, c := range suffix {
			if c < '0' || c > '9' {
				isPort = false
				break
			}
		}
		if isPort {
			s = s[:i]
		}
	}
	return strings.ToLower(strings.TrimSpace(s))
}

func parseDomain(raw string) (*domainInfo, error) {
	host := stripToHost(raw)
	if host == "" {
		return nil, fmt.Errorf("empty host")
	}

	// icann flag intentionally ignored — private PSL entries (*.github.io,
	// *.s3.amazonaws.com, etc.) are valid bug bounty targets.
	eTLD, _ := publicsuffix.PublicSuffix(host)

	apex, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return nil, err
	}

	domain := strings.TrimSuffix(apex, "."+eTLD)

	subdomain := ""
	if len(host) > len(apex) {
		subdomain = host[:len(host)-len(apex)-1]
	}

	return &domainInfo{
		Input:     raw,
		TLD:       eTLD,
		Domain:    domain,
		Subdomain: subdomain,
		Apex:      apex,
	}, nil
}

func process(line string, getTLD, getSub, getDomain, getApex, getInfo bool) string {
	// No parse flag: just emit the matched domain as-is (geturls replacement mode).
	if !getTLD && !getSub && !getDomain && !getApex && !getInfo {
		return line
	}

	info, err := parseDomain(line)
	if err != nil {
		return ""
	}

	switch {
	case getInfo:
		b, _ := json.Marshal(info)
		return string(b)
	case getTLD:
		return info.TLD
	case getSub:
		return info.Subdomain
	case getDomain:
		return info.Domain
	case getApex:
		return info.Apex
	}
	return ""
}

func isTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func main() {
	getTLD := flag.Bool("tld", false, "Return top-level domain")
	getSub := flag.Bool("sub", false, "Return subdomain")
	getDomain := flag.Bool("domain", false, "Return root domain")
	getApex := flag.Bool("apex", false, "Return apex domain (eTLD+1)")
	getInfo := flag.Bool("info", false, "Return JSON info about domain structure")
	file := flag.String("f", "", "Input file (reads from stdin if not specified)")
	concurrency := flag.Int("c", 100, "Concurrency level")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "domx — domain parser and extractor\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  cat domains.txt | domx [options]\n")
		fmt.Fprintf(os.Stderr, "  domx -f <file> [options]\n\n")
		fmt.Fprintf(os.Stderr, "Parse flags (pick one):\n")
		fmt.Fprintf(os.Stderr, "  -apex    Return apex domain (eTLD+1), e.g. example.com\n")
		fmt.Fprintf(os.Stderr, "  -domain  Return root domain label, e.g. example\n")
		fmt.Fprintf(os.Stderr, "  -sub     Return subdomain, e.g. www\n")
		fmt.Fprintf(os.Stderr, "  -tld     Return top-level domain, e.g. co.uk\n")
		fmt.Fprintf(os.Stderr, "  -info    Return full JSON breakdown\n\n")
		fmt.Fprintf(os.Stderr, "Other flags:\n")
		fmt.Fprintf(os.Stderr, "  -f <file>  Read input from file instead of stdin\n")
		fmt.Fprintf(os.Stderr, "  -c <int>   Concurrency level (default 100)\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  cat domains.txt | domx -apex\n")
		fmt.Fprintf(os.Stderr, "  domx -f domains.txt -tld\n")
		fmt.Fprintf(os.Stderr, "  domx -f report.txt               # extract domains from noisy file\n")
		fmt.Fprintf(os.Stderr, "  domx -f report.txt -apex         # extract + parse apex\n")
		fmt.Fprintf(os.Stderr, "  domx -f report.txt -info         # full JSON breakdown\n")
		fmt.Fprintf(os.Stderr, "  cat domains.txt | domx -sub | sort -u\n")
	}

	flag.Parse()

	var reader io.Reader
	if *file != "" {
		f, err := os.Open(*file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "domx: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		reader = f
	} else if !isTerminal() {
		reader = os.Stdin
	} else {
		flag.Usage()
		os.Exit(0)
	}

	lines := make(chan string, *concurrency)
	results := make(chan string, *concurrency)

	var wg sync.WaitGroup
	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for line := range lines {
				if out := process(line, *getTLD, *getSub, *getDomain, *getApex, *getInfo); out != "" {
					results <- out
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	go func() {
		scanner := bufio.NewScanner(reader)
		buf := make([]byte, 1024*1024)
		scanner.Buffer(buf, len(buf))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" && lineRegex.MatchString(line) {
				lines <- line
			}
		}
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "domx: read error: %v\n", err)
		}
		close(lines)
	}()

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	for result := range results {
		fmt.Fprintln(out, result)
	}
}
