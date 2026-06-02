package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
)

func getLinesChannel(f io.ReadCloser) <-chan string {

	out := make(chan string, 1)

	go func() {
		defer f.Close()
		defer close(out)

		str := ""
		data := make([]byte, 8)

		for {

			count, err := f.Read(data)

			if err == io.EOF {
				break
			} else if err != nil {
				log.Fatal("Padh nahi pa raha bhai: ", err)
			}

			str += string(data[:count])

			for {
				i := strings.IndexByte(str, '\n')

				if i == -1 {
					break
				}
				out <- str[:i]
				str = str[i+1:]
			}
		}

		if len(str) != 0 {
			out <- str
		}
	}()

	return out
}

func main() {
	file, err := os.Open("messages.txt")

	if err != nil {
		log.Fatal("Error agaya bhai: ", err)
	}

	lines := getLinesChannel(file)

	for line := range lines {
		fmt.Println("Line:", line)
	}
}
