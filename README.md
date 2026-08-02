# go-http

A miniature HTTP/1.1 server implemented from scratch in Go — built to deeply understand how HTTP works under the hood by reimplementing its core components: a TCP server, an HTTP request parser, a response serializer, a headers subsystem, and chunked transfer encoding.

---

## Table of Contents

- [Project Overview](#project-overview)
- [Features](#features)
- [Technologies & References](#technologies--references)
- [Project Architecture](#project-architecture)
- [Internal Workflow: A Request End-to-End](#internal-workflow-a-request-end-to-end)
- [File-by-File Breakdown](#file-by-file-breakdown)
- [Important Concepts](#important-concepts)
- [Design Decisions](#design-decisions)
- [Limitations](#limitations)
- [Future Improvements](#future-improvements)
- [Learning Outcomes](#learning-outcomes)

---

## Project Overview

### What is this project?

This is a functional HTTP/1.1 server written entirely in Go, using only the standard library (plus `testify` for tests). It accepts real browser and `curl` connections, speaks the HTTP/1.1 wire protocol, parses request lines, headers, and bodies, dispatches to a handler function, and writes well-formed HTTP responses — including chunked transfer encoding and HTTP trailers.

The server runs on port `42069`. A companion `tcplistener` binary lets you inspect raw incoming requests at the TCP level, which was useful during development for understanding the byte stream before any parsing logic existed.

### Why was this built?

Using an HTTP framework like `net/http` abstracts away everything interesting. Building an HTTP server from a raw TCP socket forces you to answer the hard questions: How does a server know where one header ends and the next begins? How does `Content-Length` work, and what happens if the body is shorter than advertised? What is chunked transfer encoding actually doing at the byte level? How do HTTP trailers fit in?

This project was built to answer those questions concretely, in code.

### What is HTTP?

HTTP (HyperText Transfer Protocol) is the application-layer protocol that powers the web. It runs over TCP and defines a structured text format for requests and responses: a status/request line, headers, a blank line separator, and an optional body. HTTP/1.1 introduced persistent connections, chunked transfer encoding, host headers, and a range of other features that make it the dominant version still in widespread use.

### Why is implementing HTTP a valuable exercise?

Building an HTTP server from scratch touches core backend engineering concepts:

- **Network programming** — accepting TCP connections, reading byte streams, understanding that TCP delivers a raw stream with no concept of message boundaries
- **Protocol parsing** — implementing a stateful, incremental parser that handles partial reads gracefully
- **State machines** — modeling the parsing lifecycle as explicit states (`init → headers → body → done`)
- **Streaming I/O** — chunked encoding, trailers, and forwarding proxied responses without buffering the full body
- **Separation of concerns** — headers, request parsing, response writing, and server lifecycle as independent, testable packages

---

## Features

| Feature | Description |
|---|---|
| HTTP/1.1 request parsing | Parses request line (method, target, version), headers, and body |
| Response writing | Status line, headers, and body written as valid HTTP/1.1 |
| Chunked transfer encoding | Streams responses without a known `Content-Length` upfront |
| HTTP trailers | Sends `X-Content-SHA256` and `X-Content-Length` after the chunked body |
| `/httpbin/*` proxy | Forwards requests to `httpbin.org` and streams the response back |
| `/video` endpoint | Serves a binary `.mp4` file with the correct `video/mp4` content type |
| Concurrent connections | Each accepted connection runs in its own goroutine |
| Graceful shutdown | `SIGINT`/`SIGTERM` triggers a clean server close via `defer` |
| Header case-insensitivity | All header names normalized to lowercase, per RFC 9110 |
| Duplicate header merging | Multiple values for the same header key are comma-joined |


---

## Technologies & References

### Go Standard Library

| Package | Why it is used |
|---|---|
| `net` | `net.Listen` and `net.Conn` for TCP server setup and per-connection I/O |
| `io` | `io.Reader`, `io.Writer`, `io.ReadWriteCloser` interfaces for stream abstraction; `io.EOF` for termination |
| `bytes` | `bytes.Index`, `bytes.Split`, `bytes.TrimSpace` for parsing the raw byte slices that make up request lines and headers |
| `fmt` | Response formatting and `fmt.Appendf` for building header byte slices |
| `strconv` | Parsing `Content-Length` integer values from header strings |
| `strings` | Prefix matching on request targets for routing |
| `crypto/sha256` | Computing a SHA-256 digest of a proxied response body, sent as an HTTP trailer |
| `net/http` | Used only as an HTTP client to forward `/httpbin/*` requests to `httpbin.org` |
| `os` | Reading the `.mp4` file for the `/video` endpoint; catching OS signals for shutdown |
| `os/signal` / `syscall` | Listening for `SIGINT` and `SIGTERM` to trigger graceful shutdown |

### External Dependency

| Package | Why it is used |
|---|---|
| `github.com/stretchr/testify` | `assert` and `require` helpers in the test suite for cleaner test assertions |

### Protocol

**HTTP/1.1** — a text-based, line-oriented request/response protocol transmitted over TCP. Requests and responses both follow the format: start line → headers → blank line (`\r\n`) → optional body. Header field names are case-insensitive. Body length is signaled either by `Content-Length` or `Transfer-Encoding: chunked`. Implementing this protocol from scratch means any standard HTTP client (`curl`, browser, `Postman`) can talk to the server without modification.

### References

- [RFC 9110 — HTTP Semantics](https://www.rfc-editor.org/info/rfc9110/) — authoritative spec for HTTP methods, headers, and status codes
- [RFC 9112 — HTTP/1.1](https://www.rfc-editor.org/info/rfc9112/) — wire format, chunked encoding, trailers
- [Go net package documentation](https://pkg.go.dev/net)
- [Go io package documentation](https://pkg.go.dev/io)
- ["Build HTTP Server from Scratch" by Boot.dev and ThePrimeagen](https://youtu.be/FknTw9bJsXM?si=wEH2YDh1Lwd2ENho) — course that guided this implementation

---

## Project Architecture

### Layer Diagram

```
┌──────────────────────────────────────┐
│          curl / browser / client     │
└─────────────────┬────────────────────┘
                  │ TCP bytes (HTTP/1.1 encoded)
                  ▼
┌──────────────────────────────────────┐
│    TCP Server  (internal/server)     │
│  net.Listen → listener.Accept()      │
│  go runConnection(conn) per client   │
└─────────────────┬────────────────────┘
                  │ io.ReadWriteCloser (net.Conn)
                  ▼
┌──────────────────────────────────────┐
│  HTTP Request Parser (internal/request) │
│  RequestFromReader(conn)             │
│  State machine: init→headers→body→done│
└──────────┬───────────────────────────┘
           │ *request.Request
           ▼
┌──────────────────────────────────────┐
│     Handler func  (cmd/httpserver)   │
│  Inspects RequestTarget, dispatches  │
│  to the right response path          │
└──────────┬───────────────────────────┘
           │
  ┌────────┴────────────────┐
  ▼                         ▼
┌───────────────┐   ┌──────────────────────┐
│  Static/HTML  │   │  Proxy / Video /     │
│  responses    │   │  Chunked streaming   │
│  (200/400/500)│   │  (httpbin / vim.mp4) │
└───────┬───────┘   └──────────┬───────────┘
        └──────────┬───────────┘
                   ▼
┌──────────────────────────────────────┐
│  Response Writer  (internal/response)│
│  WriteStatusLine → WriteHeaders      │
│  → WriteBody                         │
└──────────────────────────────────────┘
                  │ TCP bytes (HTTP/1.1 response)
                  ▼
┌──────────────────────────────────────┐
│          curl / browser / client     │
└──────────────────────────────────────┘
```

### Startup Sequence

```
main() starts
   │
   ├─ server.Serve(42069, handlerFunc)
   │     ├─ net.Listen("tcp", ":42069")   — opens TCP socket
   │     └─ go runServer(...)             — accepts connections in background
   │
   ├─ signal.Notify(sigChan, SIGINT, SIGTERM)
   └─ <-sigChan                           — blocks until OS signal received
         └─ defer s.Close()              — closes listener, goroutines exit
```

### Why each layer exists

- **TCP layer** — HTTP runs over TCP. `net.Listen` / `Accept` give us raw bidirectional byte streams per client. Everything above builds on that.
- **Request parser** — HTTP is a text protocol but arrives as a raw byte stream. The parser bridges those two worlds, handling partial reads, incremental buffering, and state transitions.
- **Headers package** — Headers are used by both requests and responses. Extracting them into their own package means the parsing and manipulation logic is shared and independently tested.
- **Handler func** — A single `func(w *response.Writer, req *request.Request)` callback is the application logic seam. The server knows nothing about routes; the handler knows nothing about TCP. Clean boundary.
- **Response writer** — Builds valid HTTP/1.1 responses byte by byte: status line first, then headers, then body. Used for both simple HTML responses and streaming chunked responses.


---

## Internal Workflow: A Request End-to-End

Let's trace what happens when a client sends `GET /httpbin/get HTTP/1.1`.

### Step 1 — Client sends bytes

`curl` serializes the request and sends it over TCP:

```
GET /httpbin/get HTTP/1.1\r\n
Host: localhost:42069\r\n
User-Agent: curl/8.19.0\r\n
Accept: */*\r\n
\r\n
```

Breaking it down:
- First line: `METHOD SP request-target SP HTTP-version CRLF`
- Each header: `name: value CRLF`
- Blank line (`\r\n` alone): signals end of headers, no body for a GET

### Step 2 — Server accepts the connection

`runServer` is looping on `listener.Accept()`. It gets a `net.Conn`, then immediately spawns `go runConnection(s, conn)` — so the loop returns to `Accept()` without waiting, enabling concurrent clients.

### Step 3 — Request parser reads the stream

`RequestFromReader(conn)` runs the stateful parser. It starts with a 1024-byte buffer that doubles when full, and loops until the request is `done`.

**State: `StateInit`** — calls `parseRequestLine`. Scans for the first `\r\n`, splits on spaces, validates `HTTP/1.1`, extracts method/target/version. Advances state to `StateHeaders`.

**State: `StateHeaders`** — delegates to `headers.Parse`. Reads field lines one by one until it hits a bare `\r\n` (the header terminator). Each field line is split on `:`, name is validated as a token, value is trimmed. Since `Content-Length` is absent on a GET, state transitions to `StateDone`.

The result:
```go
&Request{
    RequestLine: {Method: "GET", RequestTarget: "/httpbin/get", HttpVersion: "1.1"},
    Headers:     {"host": "localhost:42069", "user-agent": "curl/8.19.0", "accept": "*/*"},
    Body:        "",
    State:       StateDone,
}
```

### Step 4 — Handler dispatches

Back in `runConnection`, the handler func is called. It checks `req.RequestLine.RequestTarget`:

```go
strings.HasPrefix(req.RequestLine.RequestTarget, "/httpbin/")  // true
```

It strips the prefix and fires an outbound `http.Get("https://httpbin.org/get")`.

### Step 5 — Chunked response is streamed back

The handler doesn't know the full response size upfront, so it uses chunked transfer encoding:

1. `WriteStatusLine(StatusOK)` → `HTTP/1.1 200 OK\r\n`
2. `WriteHeaders(...)` — includes `Transfer-Encoding: chunked` and `Trailer: X-Content-SHA256, X-Content-Length`
3. Loop: reads up to 32 bytes at a time from `httpbin.org`'s response body. For each chunk: writes the hex length, `\r\n`, the data, `\r\n`
4. Final `0\r\n` — the chunked terminator signaling end of body
5. Trailers written: `X-Content-SHA256` (SHA-256 of full accumulated body) and `X-Content-Length` (total byte count)

### Step 6 — Client receives and decodes

`curl` reads the chunked response, reassembles the body, reads the trailers, and displays the result.

---

### POST with a body

For `POST /submit` with `Content-Length: 13` and body `hello world!\n`:

- Parser hits `StateBody` after headers are done
- `hasBody()` checks `Content-Length > 0` → true
- `StateBody` appends bytes from the buffer into `r.Body` until `len(r.Body) == contentLength`
- Transitions to `StateDone`

If the connection closes before enough bytes arrive, `RequestFromReader` returns `ERROR_UNEXPECTED_EOF`.


---

## File-by-File Breakdown

### `cmd/httpserver/main.go` — Server Entry Point and Request Handler

**Responsibility:** Bootstraps the server and defines the application-level routing and response logic as a single handler closure.

**Routes:**

| Path | Behavior |
|---|---|
| `/` (default) | Returns a `200 OK` HTML page |
| `/yourproblem` | Returns a `400 Bad Request` HTML page |
| `/myproblem` | Returns a `500 Internal Server Error` HTML page |
| `/video` | Reads `assets/vim.mp4` from disk and serves it as `video/mp4` |
| `/httpbin/*` | Proxies the sub-path to `httpbin.org` and streams the response with chunked encoding + trailers |

**Key logic:**
- `server.Serve(port, handlerFunc)` is the only setup call — no boilerplate listener management in `main`.
- `signal.Notify` catches `SIGINT`/`SIGTERM` so the process doesn't just die on Ctrl-C; `defer s.Close()` runs cleanup.
- The `/httpbin/` branch demonstrates chunked streaming: reads 32 bytes at a time, writes each chunk in `<hex-len>\r\n<data>\r\n` format, and appends SHA-256 and length trailers after the terminating `0\r\n`.

---

### `cmd/tcplistener/main.go` — Raw TCP Inspector

**Responsibility:** A minimal debug binary that listens on the same port, accepts one connection at a time, parses the request with `RequestFromReader`, and prints the structured output to stdout. It was used during development to verify the parser before the full server existed.

---

### `internal/server/server.go` — TCP Server Lifecycle

**Responsibility:** Owns the TCP listener, accepts connections in a loop, and dispatches each to a goroutine. Provides a `Close()` method for graceful shutdown.

**Important types:**

```go
type Handler func(w *response.Writer, req *request.Request)

type Server struct {
    closed   bool
    handler  Handler
    listener net.Listener
}
```

**Important functions:**

| Function | What it does |
|---|---|
| `Serve(port, handler)` | Opens the TCP listener, creates the Server, spawns the accept loop goroutine, returns immediately |
| `runServer(s, listener)` | Accept loop — calls `listener.Accept()` in a `for` loop; spawns `go runConnection` per connection; exits when `s.closed` is true |
| `runConnection(s, conn)` | Parses the request; on error sends a 400; otherwise calls `s.handler`; `defer conn.Close()` ensures cleanup |
| `s.Close()` | Sets `closed = true`, calls `listener.Close()` — unblocks `Accept()` so `runServer` exits cleanly |

**Why this file exists:** Separates the server lifecycle (TCP, goroutines, shutdown) from both the protocol (request parsing) and the application (handler logic).

---

### `internal/request/request.go` — HTTP Request Parser

**Responsibility:** Implements the full HTTP/1.1 request parsing pipeline: incremental buffering, state machine transitions, request line parsing, header delegation, and body accumulation.

**Important types:**

```go
type Request struct {
    RequestLine RequestLine    // method, target, HTTP version
    Headers     *headers.Headers
    Body        string
    State       parseState     // tracks progress through the state machine
}

type RequestLine struct {
    HttpVersion   string
    RequestTarget string
    Method        string
}

type parseState string  // "init" | "headers" | "body" | "done" | "error"
```

**State machine:**

```
StateInit ──► StateHeaders ──► StateDone   (no body)
                           └──► StateBody ──► StateDone
```

Any parse error transitions to `StateError`, which causes `RequestFromReader` to return the error immediately on the next `parse` call.

**Important functions:**

| Function | What it does |
|---|---|
| `RequestFromReader(reader)` | Owns the read loop: allocates a 1024-byte buffer that doubles when full, calls `reader.Read`, then `request.parse` on accumulated bytes, sliding unconsumed bytes to the front of the buffer |
| `request.parse(data)` | Runs the state machine over whatever bytes are currently in the buffer; returns how many bytes were consumed |
| `parseRequestLine(data)` | Scans for `\r\n`, splits on spaces, validates `HTTP/` prefix and `1.1` version |
| `request.hasBody()` | Checks `Content-Length > 0` to decide whether to enter `StateBody` |

**Why this file exists:** HTTP request parsing is non-trivial because TCP delivers data in arbitrary chunks. The state machine + sliding buffer approach handles partial reads correctly without requiring the full request to be in memory before parsing starts.

---

### `internal/headers/headers.go` — Header Storage and Parsing

**Responsibility:** Provides a case-insensitive header store with RFC-compliant field-name validation, value accumulation for duplicate keys, and an incremental `Parse` method shared by both request reading and (via `GetDefaultHeaders`) response writing.

**Important type:**

```go
type Headers struct {
    headers map[string]string  // all keys stored lowercase
}
```

**Important functions:**

| Function | What it does |
|---|---|
| `Parse(data)` | Scans `data` for `\r\n`-delimited field lines; stops at bare `\r\n` (end of headers); returns bytes consumed, a `done` bool, and any parse error |
| `Set(name, value)` | Comma-joins value onto an existing entry (handles `Host: a\r\nHost: b` → `"a,b"`) |
| `Replace(name, value)` | Overwrites any existing value — used when a known header needs to be updated |
| `Delete(name)` | Removes a header key — used when switching from `Content-Length` to `Transfer-Encoding: chunked` |
| `Get(name)` | Case-insensitive lookup |
| `ForEach(cb)` | Iterates all entries — used by `WriteHeaders` to serialize to bytes |
| `isToken(b)` | Validates a header field name against the RFC 9110 token character set |

**Why this file exists:** Headers appear in both directions. Keeping the header map in its own package avoids duplicating the parsing and case-normalization logic between request and response paths.

---

### `internal/response/response.go` — HTTP Response Writer

**Responsibility:** Writes a valid HTTP/1.1 response to any `io.Writer` in three steps: status line, headers, body. Provides the default header set and the `StatusCode` constants.

**Important types:**

```go
type StatusCode int  // 200, 400, 500

type Writer struct {
    writer io.Writer
}
```

**Important functions:**

| Function | What it does |
|---|---|
| `GetDefaultHeaders(contentLen)` | Returns a `Headers` with `Content-Length`, `Connection: close`, and `Content-Type: text/plain` pre-set |
| `WriteStatusLine(code)` | Writes e.g. `HTTP/1.1 200 OK\r\n` |
| `WriteHeaders(h)` | Iterates headers, writes `name: value\r\n` for each, then a final `\r\n` |
| `WriteBody(p)` | Writes raw bytes — called once for static responses, multiple times for chunked streaming |

**Why this file exists:** Response serialization is its own concern. The writer knows nothing about routing or parsing — it only knows how to emit bytes in the right order.


---

## Important Concepts

### TCP and Message Boundaries

TCP is a byte-stream protocol. It delivers bytes in order and without loss, but it has no concept of "messages". A `curl` request might arrive as one big chunk, or as dozens of tiny fragments depending on network conditions, kernel buffering, and MTU. This is why `RequestFromReader` uses a loop with a growing buffer rather than a single `Read` call — it accumulates bytes and feeds them into the parser incrementally, consuming what was parsed and sliding the remainder to the front.

### HTTP/1.1 Wire Format

An HTTP/1.1 request looks like this on the wire:

```
METHOD SP request-target SP HTTP/1.1 CRLF
Header-Name: header-value CRLF
Header-Name: header-value CRLF
CRLF
[body bytes]
```

The blank line (`\r\n` with no preceding content) is the boundary between headers and body. There is no length prefix for the header section — the parser must scan for it. Body length is communicated either by `Content-Length` (exact byte count) or `Transfer-Encoding: chunked` (self-framing chunks).

### Stateful Incremental Parsing

The request parser uses an explicit state machine because partial reads are the norm. If a `\r\n` hasn't arrived yet, the parser can't finish the current line — it returns the bytes consumed so far and waits for more data. On the next iteration, the same data (minus what was consumed) is presented again. This approach is allocation-efficient and handles any chunk size, including 1 byte at a time (which the `chunkReader` in tests exercises directly).

### Chunked Transfer Encoding

When a response body's total size isn't known upfront — for example, when proxying a live HTTP response — `Content-Length` can't be set. Chunked transfer encoding solves this by prefixing each piece of the body with its hexadecimal byte count:

```
HTTP/1.1 200 OK\r\n
Transfer-Encoding: chunked\r\n
\r\n
1f\r\n
...31 bytes of body data...\r\n
0\r\n
\r\n
```

The `0\r\n\r\n` sequence signals the end. The client reassembles the body from the chunks. This is what the `/httpbin/*` proxy path uses — it reads 32 bytes at a time from the upstream response and immediately writes each as a chunk to the client.

### HTTP Trailers

Trailers are headers sent *after* the chunked body, declared in advance with a `Trailer:` header. They are used when metadata about the body (like a checksum or total size) can only be computed after the full body has been processed. The `/httpbin/*` handler accumulates the full body while streaming it, then appends `X-Content-SHA256` and `X-Content-Length` as trailers:

```
0\r\n
X-Content-SHA256: a3f9...\r\n
X-Content-Length: 512\r\n
\r\n
```

### Header Case-Insensitivity

RFC 9110 states header field names are case-insensitive. `Headers` normalizes all names to lowercase on entry (`strings.ToLower`), so `Content-Length`, `content-length`, and `CONTENT-LENGTH` all map to the same key. This prevents subtle bugs where a lookup fails because the case doesn't match.

### Header Token Validation

RFC 9110 defines a strict character set for header field names (called "tokens"): letters, digits, and a set of allowed symbols. Characters like `©` or spaces are not valid token characters. `isToken` enforces this — if a header name contains an illegal character, `Parse` returns an error rather than silently accepting a malformed request.

### Duplicate Header Handling

When the same header name appears multiple times (e.g., two `Host:` lines), RFC 9110 says their values should be treated as a comma-separated list. `Headers.Set` implements this: if the key already exists, the new value is appended with a comma. `Headers.Replace` is the override variant — used when you explicitly want to overwrite a value (like swapping out `Content-Length` when switching to chunked mode).

### Goroutines Per Connection

`runServer` calls `go runConnection(s, conn)` for each accepted connection. This is the standard Go pattern for concurrent servers: one goroutine per connection, cheap to create, no thread pool management required. Each goroutine runs independently and exits when `runConnection` returns (which closes the connection via `defer conn.Close()`).

### Graceful Shutdown

The server uses `signal.Notify` to catch `SIGINT` (Ctrl-C) and `SIGTERM` (process manager stop). When received, `s.Close()` sets `closed = true` and calls `listener.Close()`, which unblocks the `Accept()` call in `runServer`. The `closed` flag prevents a spurious error log from the now-closed listener. In-flight connections finish naturally because their goroutines are independent.


---

## Design Decisions

### Handler as a plain function, not an interface

**What was chosen:** `server.Serve` accepts a `Handler` which is just `func(w *response.Writer, req *request.Request)`. The entire routing and response logic lives in that single closure in `main.go`.

**Alternatives:** An `http.Handler`-style interface, or a router struct with `Handle(pattern, fn)` registration.

**Why this choice:** The server is a learning project, not a framework. A plain function type is the simplest possible seam between server infrastructure and application logic. It keeps the `server` package minimal and makes the handler trivially testable — just call it with a fake writer and request.

---

### Growing buffer with sliding window in `RequestFromReader`

**What was chosen:** Start with a 1024-byte buffer. After each `parse` call, slide unread bytes to the front (`copy(buf, buf[readN:bufLen])`). If the buffer fills completely, double it.

**Alternatives:** `bufio.Reader` (Go's built-in buffered reader), or allocating a fresh buffer per read.

**Why this choice:** Building the buffer manually makes the mechanics of incremental parsing visible — it's the point of the exercise. It also demonstrates the real concern: a header or body that exceeds any fixed buffer must be handled, which is what the doubling strategy addresses.

**Trade-off:** `bufio.Reader` would be simpler and more idiomatic in production code. The comment in `request.go` acknowledges this explicitly.

---

### State machine as explicit string constants

**What was chosen:** `parseState` is a `string` type with named constants (`StateInit`, `StateHeaders`, `StateBody`, `StateDone`, `StateError`). The `parse` method is a `for` + `switch`.

**Alternatives:** An integer enum, a set of boolean flags, or a function-pointer chain.

**Why this choice:** String constants are self-documenting in debug output and logs. The `switch` on `parseState` reads exactly like a state machine diagram. Adding a new state is one constant and one case.

---

### `Headers` as a separate package shared by request and response

**What was chosen:** `internal/headers` is its own package, imported by both `request` and `response`.

**Alternatives:** Embed header handling inline in `request.go`, and use a separate simpler struct in `response`.

**Why this choice:** Headers have real logic: token validation, case normalization, duplicate merging, CRLF-delimited parsing. That logic belongs in one place. Sharing it also means the same test suite (`headers_test.go`) validates headers regardless of which direction they travel.

---

### Response writer with explicit `WriteStatusLine → WriteHeaders → WriteBody` ordering

**What was chosen:** Three separate methods that must be called in order. No enforcement at the type level.

**Alternatives:** A single `Write(status, headers, body)` method, or a builder pattern that enforces ordering.

**Why this choice:** HTTP responses are inherently ordered — status line must come first, headers before body. Making this a three-step call sequence mirrors the actual wire format and makes the streaming case natural: you can call `WriteBody` multiple times for chunked responses, then call `WriteHeaders` again for trailers.

---

### Chunked proxy reads 32 bytes at a time

**What was chosen:** `make([]byte, 32)` in the proxy loop — each upstream read produces a chunk of up to 32 bytes.

**Alternatives:** Larger buffers (4096, 8192) for fewer syscalls; `io.Copy` with a custom writer.

**Why this choice:** The small chunk size makes the chunked encoding behavior clearly visible in `curl --verbose` output — you can see many small chunks arriving. It's a deliberate pedagogy choice, not a performance one. In a real proxy, 4-8KB chunks are typical.


---

## Limitations

This is an intentionally scoped implementation. The following are known limitations compared to a production HTTP server:

| Limitation | Detail |
|---|---|
| HTTP/1.1 only | HTTP/2 and HTTP/3 are not supported. No multiplexing, no header compression. |
| No keep-alive | `Connection: close` is sent on every response. Each request requires a new TCP connection. |
| No chunked request bodies | Incoming `Transfer-Encoding: chunked` request bodies are not parsed. Only `Content-Length` is handled. |
| No TLS / HTTPS | All traffic is plaintext. Adding TLS would require `tls.Listen` and a certificate. |
| No router | Routing is a chain of `if/else` blocks in a single handler closure. No pattern matching, no path parameters. |
| No middleware | No request logging, authentication, or compression layers. |
| No request body streaming | The full body is accumulated into a `string` before the handler is called. Large bodies will buffer entirely in memory. |
| Limited status codes | Only 200, 400, and 500 are implemented. 301, 404, 405, 429, etc. are absent. |
| No multipart or form parsing | Raw body bytes are stored as a string. No `multipart/form-data` or `application/x-www-form-urlencoded` parsing. |
| Error handling in `RequestFromReader` | The TODO comment in the read loop notes that some error paths are incomplete. |
| Debug `fmt.Println` in parser | `RequestFromReader` prints `bufLen` and capacity on every read — leftover instrumentation from development. |
| No timeouts | No read deadline, write deadline, or idle connection timeout. A slow client can hold a goroutine indefinitely. |

---

## Future Improvements

These are realistic next steps, ordered roughly by foundational importance:

### Router with pattern matching
Replace the `if/else` chain with a `map[string]Handler` or a simple trie. Support path parameters like `/users/:id`. This is what every Go web framework (Chi, Gin, Echo) builds first.

### Keep-alive connections
Remove `Connection: close` and loop on `RequestFromReader` within `runConnection` to serve multiple requests per TCP connection. This is the default behavior for HTTP/1.1 and eliminates the TCP handshake cost on every request.

### Chunked request body parsing
Add a `StateChunked` to the request state machine that reads chunk size lines and accumulates body bytes accordingly. This allows receiving `Transfer-Encoding: chunked` uploads.

### TLS support
Wrap `net.Listen` with `tls.Listen` and load a certificate. HTTPS is a thin layer on top of the same HTTP parsing — the only change is who provides the `io.ReadWriteCloser`.

### Request timeouts
Set `conn.SetDeadline(time.Now().Add(timeout))` on each accepted connection. Prevents slow-loris style attacks and runaway goroutines.

### Streaming request body to handler
Instead of buffering `Body` as a `string`, expose it as an `io.Reader`. This lets handlers stream large uploads without holding them in memory.

### Middleware chain
Add a `Middleware func(Handler) Handler` type and a `Chain(...Middleware)` helper. Common concerns like request logging, panic recovery, and authentication become composable layers rather than embedded in the handler.

### Additional status codes
Implement 301 (redirect), 404 (not found), 405 (method not allowed), 429 (rate limit), 503 (unavailable). Most real servers need at least these.

### Remove debug instrumentation
The `fmt.Println("bufLen:", bufLen)` calls in `RequestFromReader` should be removed or gated behind a debug flag before any real use.

---

## Learning Outcomes

Building this project demonstrates concrete hands-on experience with:

**Network programming** — Setting up a TCP server, accepting connections, and reading from a streaming byte source. Internalizing that TCP has no message boundaries and that the application protocol must define them.

**HTTP/1.1 wire format** — Parsing request lines and headers byte-by-byte. Understanding CRLF terminators, the blank-line separator, `Content-Length` body framing, and chunked transfer encoding at the implementation level rather than the conceptual level.

**Stateful incremental parsing** — Building a state machine that handles partial reads gracefully. Understanding why parsers must be re-entrant: data can arrive in any chunk size, and the parser must resume where it left off.

**Chunked transfer encoding and trailers** — Implementing the `<hex-len>\r\n<data>\r\n` format and terminating `0\r\n` sequence. Computing and appending trailers after the body is fully processed.

**Buffered I/O and sliding windows** — Managing a dynamic read buffer, sliding unprocessed bytes to the front after each parse pass, and doubling capacity when the buffer is exhausted.

**Goroutines per connection** — The standard Go pattern for concurrent servers. Each connection is independent; the server scales naturally to multiple clients without a thread pool.

**Separation of concerns** — Four distinct internal packages (`headers`, `request`, `response`, `server`), each with a single well-defined responsibility and no knowledge of the others beyond its direct dependency.

**Go idioms** — Function types as first-class values for the handler callback, `defer` for connection cleanup, `io.Reader`/`io.Writer` interfaces for I/O abstraction, and struct methods for encapsulation.

---

## Running the Project

**Prerequisites:** Go 1.21+

```bash
# Clone
git clone https://github.com/Aayushman-nvm/Go-HTTP.git
cd Go-HTTP

# Run the HTTP server (port 42069)
go run ./cmd/httpserver

# In a separate terminal — basic request
curl -v http://localhost:42069/

# Test a 400 response
curl -v http://localhost:42069/yourproblem

# Test a 500 response
curl -v http://localhost:42069/myproblem

# Proxy to httpbin.org with chunked encoding and trailers
curl -v http://localhost:42069/httpbin/get

# Serve a video file
curl -v http://localhost:42069/video --output out.mp4

# Run the raw TCP inspector (separate binary)
go run ./cmd/tcplistener
curl http://localhost:42069/

# Run the test suite
go test ./...
```

---

*Built with Go's standard library only. No external dependencies beyond `testify` for tests.*
