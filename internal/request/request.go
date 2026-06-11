package request

import (
	"bytes"
	"fmt"
	"io"
	"strconv"

	"github.com/Aayushman-nvm/Go-HTTP.git/internal/headers"
)

type Request struct {
	RequestLine RequestLine
	Headers     *headers.Headers
	Body        string
	State       parseState
}

type RequestLine struct {
	HttpVersion   string
	RequestTarget string
	Method        string
}

type parseState string

const (
	StateInit    parseState = "init"
	StateHeaders parseState = "headers"
	StateBody    parseState = "body"
	StateDone    parseState = "done"
	StateError   parseState = "error"
)

func getInt(headers *headers.Headers, name string, defaultValue int) int {
	valueStr, exists := headers.Get(name)
	if !exists {
		return defaultValue
	}
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return defaultValue
	}
	return value
}

func newRequest() *Request {
	return &Request{
		State:   StateInit,
		Headers: headers.NewHeaders(),
		Body:    "",
	}
}

var ERROR_MALFORMED_REQUEST_LINE = fmt.Errorf("Malformed request line")
var ERROR_UNSUPPORTED_HTTP_VERSION = fmt.Errorf("Unsupported http version")
var ERROR_REQUEST_IN_ERROR_STATE = fmt.Errorf("Request in error state")
var ERROR_UNEXPECTED_EOF = fmt.Errorf("Body shorter than content-length")
var SEPARATOR = []byte("\r\n")

func parseRequestLine(str []byte) (*RequestLine, int, error) {

	idx := bytes.Index(str, SEPARATOR)
	if idx == -1 {
		return nil, 0, nil
	}

	startLine := str[:idx]
	read := idx + len(SEPARATOR)

	parts := bytes.Split(startLine, []byte(" "))
	if len(parts) != 3 {
		return nil, 0, ERROR_MALFORMED_REQUEST_LINE
	}

	httpParts := bytes.Split(parts[2], []byte("/"))
	if len(httpParts) != 2 || string(httpParts[0]) != "HTTP" {
		return nil, 0, ERROR_MALFORMED_REQUEST_LINE
	}

	if string(httpParts[1]) != "1.1" {
		return nil, 0, ERROR_UNSUPPORTED_HTTP_VERSION
	}

	reqLine := &RequestLine{
		Method:        string(parts[0]),
		RequestTarget: string(parts[1]),
		HttpVersion:   string(httpParts[1]),
	}

	return reqLine, read, nil

}

func (r *Request) hasBody() bool {
	length := getInt(r.Headers, "content-length", 0)
	return length > 0
}

func (r *Request) parse(data []byte) (int, error) {

	read := 0
outer:
	for {
		currentData := data[read:]
		switch r.State {
		case StateError:
			return 0, ERROR_REQUEST_IN_ERROR_STATE
		case StateInit:
			reqLine, n, err := parseRequestLine(currentData)
			if err != nil {
				r.State = StateError
				return 0, err
			}

			if n == 0 {
				break outer
			}

			r.RequestLine = *reqLine
			read += n
			r.State = StateHeaders

		case StateHeaders:
			n, done, err := r.Headers.Parse(currentData)
			if err != nil {
				r.State = StateError
				return 0, err
			}
			if n == 0 {
				break outer
			}
			read += n
			if done {
				if r.hasBody() {
					r.State = StateBody
				} else {
					r.State = StateDone
				}
			}
		case StateBody:
			length := getInt(r.Headers, "content-length", 0)
			if length == 0 {
				panic("chuncked not implemented")
			}

			if len(currentData) == 0 {
				break outer
			}

			remaining := min(length-len(r.Body), len(currentData))
			if remaining == 0 {
				break outer
			}
			r.Body += string(currentData[:remaining])
			read += remaining

			if len(r.Body) == length {
				r.State = StateDone
			}

		case StateDone:
			break outer
		default:
			panic("panic main garbadi kardi")
		}
	}
	return read, nil
}

func (r *Request) done() bool {
	return r.State == StateDone || r.State == StateError
}

func RequestFromReader(reader io.Reader) (*Request, error) {
	request := newRequest()

	//NOTE: buffer could get overrun by a header or body that exeeds 1k
	buf := make([]byte, 1024)
	bufLen := 0

	for !request.done() {
		//thats why we dynamically grow it here... amazing stuff
		if bufLen == len(buf) {
			newBuf := make([]byte, len(buf)*2)
			copy(newBuf, buf)
			buf = newBuf
		}
		fmt.Println("bufLen:", bufLen)
		fmt.Printf("bufLen=%d cap=%d\n", bufLen, len(buf))
		n, err := reader.Read(buf[bufLen:])
		//TODO: errors related stuff

		bufLen += n

		if n > 0 {
			readN, err := request.parse(buf[:bufLen])
			if err != nil {
				return nil, err
			}

			copy(buf, buf[readN:bufLen])
			bufLen -= readN
		}

		if err == io.EOF {
			if !request.done() {
				return nil, ERROR_UNEXPECTED_EOF
			}
			break
		}

		if err != nil {
			return nil, err
		}

	}

	return request, nil
}
