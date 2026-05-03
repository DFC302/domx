# domx

Fast domain parser and extractor written in Go. Parses TLD, subdomain, root domain, and apex from domain lists or noisy files. Replaces slow Python `tld`-based parsers and shell `geturls` pipelines.

## Install

```bash
go install github.com/DFC302/domx@latest
```

Requires Go 1.21+. The binary is placed in `$GOPATH/bin` (usually `~/go/bin`).

## Usage

```
domx — domain parser and extractor

Usage:
  cat domains.txt | domx [options]
  domx -f <file> [options]

Parse flags (pick one):
  -apex    Return apex domain (eTLD+1), e.g. example.com
  -domain  Return root domain label, e.g. example
  -sub     Return subdomain, e.g. www
  -tld     Return top-level domain, e.g. co.uk
  -info    Return full JSON breakdown

Other flags:
  -f <file>  Read input from file instead of stdin
  -c <int>   Concurrency level (default 100)
```

## Examples

### Clean domain list

```bash
# Get apex domain from a list
cat domains.txt | domx -apex

# Get TLD via file flag
domx -f domains.txt -tld

# Get unique subdomains
cat domains.txt | domx -sub | sort -u

# Full JSON breakdown
cat domains.txt | domx -info
```

### Noisy file (mixed content)

Lines that don't look like domains are silently skipped — no pre-filtering needed.

```bash
# Extract only the domains from a noisy file
domx -f report.txt

# Extract domains and parse apex in one step
domx -f report.txt -apex

# Works with stdin too
cat report.txt | domx -tld
```

### Input formats supported

Both bare domains and full URLs are handled:

```
www.example.co.uk
https://sub.example.com/path?q=1
example.com:8080
http://api.target.io
```

### Output examples

```bash
$ echo "www.example.co.uk" | domx -apex
example.co.uk

$ echo "www.example.co.uk" | domx -tld
co.uk

$ echo "www.example.co.uk" | domx -sub
www

$ echo "www.example.co.uk" | domx -domain
example

$ echo "www.example.co.uk" | domx -info
{"input":"www.example.co.uk","tld":"co.uk","domain":"example","subdomain":"www","apex":"example.co.uk"}
```

## Notes

- Uses the [Public Suffix List](https://publicsuffix.org/) via `golang.org/x/net/publicsuffix` — handles multi-part TLDs like `co.uk`, `com.au`, etc. correctly.
- Non-ICANN TLDs (private domains like `.local`) are skipped.
- When no parse flag is given, `domx` acts as a pure extractor — outputting matched domain lines as-is.
- Multiple parse flags in one invocation are not supported; the first matching flag wins.
