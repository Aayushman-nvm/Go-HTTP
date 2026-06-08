package main

import (
	"fmt"
	"log"
	"net"

	"github.com/Aayushman-nvm/Go-HTTP.git/internal/request"
)

func main() {
	listener, err := net.Listen("tcp", ":42069")

	if err != nil {
		log.Fatal("Error agaya bhai: ", err)
	}

	fmt.Println("TCP server listening on :42069")

	for {
		conn, err := listener.Accept()

		if err != nil {
			log.Fatal("Error agaya bhai: ", err)
		}

		fmt.Println("Client connected:", conn.RemoteAddr())

		req, err := request.RequestFromReader(conn)

		if err != nil {
			log.Fatal("Couldnt send request", req)
		}

		fmt.Printf("Request Line:\n")
		fmt.Printf("- Method: %s\n", req.RequestLine.Method)
		fmt.Printf("- Target: %s\n", req.RequestLine.RequestTarget)
		fmt.Printf("- Version: %s\n", req.RequestLine.HttpVersion)
		fmt.Printf("Headers:\n")
		req.Headers.ForEach(func(n, v string) {
			fmt.Printf("- %s: %s\n", n, v)
		})
		fmt.Printf("Body:\n")
		fmt.Printf("%s\n", req.Body)
	}

}
