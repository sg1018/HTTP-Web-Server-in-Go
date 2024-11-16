package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func main() {

	var directory, host string
	var port int

	flag.StringVar(&host, "host", "0.0.0.0", "interface ip/host")
	flag.IntVar(&port, "port", 4221, "tcp port to listen for connections")
	flag.StringVar(&directory, "directory", ".", "directory from which to serve files")
	flag.Parse()

	info, err := os.Stat(directory)
	if err != nil {
		fmt.Printf("Failed to check directory path: %v\n", err)
		os.Exit(1)
	}
	if !info.IsDir() {
		fmt.Printf("Invalid directory path %s\n", directory)
		os.Exit(1)
	}

	protocol := "tcp"
	address := fmt.Sprintf("%s:%d", host, port)

	listener, err := net.Listen(protocol, address)
	if err != nil {
		fmt.Printf("Failed to bind to port %d\n", port)
		os.Exit(1)
	}
	defer listener.Close()

	fmt.Printf("Listening for connections on %s\n", address)
	fmt.Printf("Serving files from %s\n", directory)

	for {
		connection, err := listener.Accept()
		if err != nil {
			fmt.Printf("Error accepting connection: %v\n", err)
		}
		fmt.Printf("Client connected %v\n", connection.RemoteAddr())
		go handleConnection(connection, directory)
	}

}

func handleConnection(connection net.Conn, directory string) {

	defer connection.Close()

	var requestMethod, requestPath, requestVersion string
	bytesRead, _ := fmt.Fscanf(connection, "%s %s %s\r\n", &requestMethod, &requestPath, &requestVersion)
	fmt.Printf("Read %d bytes from client\n", bytesRead)

	var statusCode int
	var statusMessage, responseBody, requestFilePath, extraHeaders string
	if requestVersion != "HTTP/1.1" {
		statusCode, statusMessage = 400, "Bad Request"
	} else {
		statusCode, statusMessage = 200, "OK"
		if requestMethod != "GET" && requestMethod != "POST" {
			statusCode, statusMessage = 501, "Not Implemented"
		} else if requestPath == "/" {
			// do nothing
		} else if requestPath == "/user-agent" {
			scanner := bufio.NewScanner(connection)
			for scanner.Scan() {
				line := scanner.Text()
				if strings.HasPrefix(line, "User-Agent: ") {
					responseBody = line[12:]
					break
				}
			}
		} else if strings.HasPrefix(requestPath, "/echo/") {
			responseBody = requestPath[6:]
			scanner := bufio.NewScanner(connection)
			var acceptEncodings string
			fmt.Println("processing headers...")
			for scanner.Scan() {
				line := scanner.Text()
				fmt.Println(line)
				if strings.HasPrefix(line, "Accept-Encoding: ") {
					acceptEncodings = line[17:]
				} else if line == "" {
					break
				}
			}
			fmt.Println("headers processed!")
			for _, acceptEncoding := range strings.Split(acceptEncodings, ",") {
				if strings.Trim(acceptEncoding, " ") == "gzip" {
					extraHeaders = "Content-Encoding: gzip\r\n"
					buf := bytes.NewBuffer([]byte{})
					writer := gzip.NewWriter(buf)
					writer.Write([]byte(responseBody))
					writer.Close()
					responseBody = buf.String()
					break
				}
			}
		} else if strings.HasPrefix(requestPath, "/files/") {
			requestFilePath = requestPath[7:]
			fullFilePath := filepath.Join(directory, requestFilePath)
			switch requestMethod {
			case "GET":
				statusCode, statusMessage = handleFileRequest(connection, fullFilePath)
				if statusCode == 200 {
					return
				}
			case "POST":
				statusCode, statusMessage = handleFileUpload(connection, fullFilePath)
			}
		} else {
			statusCode, statusMessage = 404, "Not Found"
		}
	}

	fmt.Println(statusCode, responseBody)
	httpResponse := fmt.Sprintf("HTTP/1.1 %d %s\r\n%sContent-Type: text/plain\r\nContent-Length: %d\r\n\r\n%s\r\n",
		statusCode, statusMessage, extraHeaders, len(responseBody), responseBody)
	bytesSent, err := connection.Write([]byte(httpResponse))
	if err != nil {
		fmt.Printf("Error sending response: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Sent %d bytes to client (expected: %d)\n", bytesSent, len(httpResponse))

}

func handleFileRequest(connection net.Conn, path string) (statusCode int, statusMessage string) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 404, "Not Found"
		}
		return 500, "Internal Server Error"
	}

	if info.IsDir() {
		return 500, "Internal Server Error"
	}

	file, err := os.Open(path)
	if err != nil {
		return 500, "Internal Server Error"
	}
	defer file.Close()

	size, _ := file.Seek(0, io.SeekEnd)
	statusCode, statusMessage = 200, "OK"
	httpHeader := fmt.Sprintf("HTTP/1.1 %d %s\r\nContent-Type: application/octet-stream\r\nContent-Length: %d\r\n\r\n",
		statusCode, statusMessage, size)
	_, err = connection.Write([]byte(httpHeader))
	if err != nil {
		fmt.Printf("Error sending response: %v\n", err)
	}

	// TODO: use copy?
	file.Seek(0, io.SeekStart)
	data := make([]byte, size)
	_, err = file.Read(data)
	if err != nil {
		return 500, "Internal Server Error"
	}

	_, err = connection.Write(data)
	if err != nil {
		fmt.Printf("Error sending response: %v\n", err)
	}
	fmt.Printf("Served file %s to client\n", path)

	return
}

func handleFileUpload(connection net.Conn, path string) (statusCode int, statusMessage string) {
	file, err := os.Create(path)
	if err != nil {
		return 500, "Internal Server Error"
	}
	defer file.Close()

	// TODO: Should actually parse headers, just getting content length for now
	var contentLength int
	reader := bufio.NewReader(connection)
	for {
		line, _ := reader.ReadString('\n')
		line = strings.TrimSuffix(line, "\r\n")
		if len(line) == 0 {
			break
		}
		if strings.HasPrefix(line, "Content-Length: ") {
			contentLength, _ = strconv.Atoi(line[16:])
		}
	}

	// TODO: use copy?
	data := make([]byte, contentLength)
	bytesRead, err := reader.Read(data)
	if err != nil {
		fmt.Printf("Error reading connection stream: %v\n", err)
		return 500, "Internal Server Error"
	}
	file.Write(data[:bytesRead])
	fmt.Printf("Received file %s from client (bytes %v)\n", path, bytesRead)

	return 201, "Created"
}
