package main

import (
	"context"
	"crypto/tls"
	markdown "github.com/MichaelMure/go-term-markdown"
	"github.com/thejerf/suture/v4"
	"golang/client/admin"
	"golang/proto"
	"io"
	"log"
	"net"
	"strconv"
	"sync"
)

var ControlConnections map[string]net.Conn
var tunnels map[string]net.Conn

const usage = "Welcome to JTunnel .\n\nSource code is at `https://github.com/manoj-inukolunu/jtunnel-go`\n\nTo create a new tunnel\n\nMake a `POST` request to `http://127.0.0.1:1234/create`\nwith the payload\n\n```\n{\n    \"HostName\":\"myhost\",\n    \"TunnelName\":\"Tunnel Name\",\n    \"localServerPort\":\"3131\"\n}\n\n```\n\nThe endpoint you get is `https://myhost.migtunnel.net`\n\nAll the requests to `https://myhost.migtunnel.net` will now\n\nbe routed to your server running on port `3131`\n\n"

type Main struct {
}

func (i *Main) Serve(ctx context.Context) error {
	result := markdown.Render(usage, 80, 6)
	log.Println(string(result))
	ControlConnections = make(map[string]net.Conn)
	tunnels = make(map[string]net.Conn)
	log.Println("Starting Admin Server on ", 1234)
	go admin.StartServer(1234)
	startControlConnection()
	return nil
}

func (i *Main) Stop() {
	log.Println("Stopping Client")
}

func main() {
	supervisor := suture.NewSimple("Client")
	service := &Main{}
	ctx, cancel := context.WithCancel(context.Background())
	supervisor.Add(service)
	errors := supervisor.ServeBackground(ctx)
	log.Println(<-errors)
	cancel()
}

func createNewTunnel(message *proto.Message) net.Conn {
	conf := &tls.Config{
		//InsecureSkipVerify: true,
	}
	conn, _ := tls.Dial("tcp", "manoj.migtunnel.net:2121", conf)
	mutex := sync.Mutex{}
	mutex.Lock()
	tunnels[message.TunnelId] = conn
	mutex.Unlock()
	proto.SendMessage(message, conn)
	return conn
}

func createLocalConnection() net.Conn {
	conn, _ := net.Dial("tcp", "localhost:3131")
	return conn
}

func startControlConnection() {
	log.Println("Starting Control connection")
	conf := &tls.Config{
		//InsecureSkipVerify: true,
	}
	conn, err := tls.Dial("tcp", "manoj.migtunnel.net:9999", conf)
	if err != nil {
		log.Println("Failed to establish control connection ", err.Error())
		panic(err)
	}
	mutex := sync.Mutex{}
	mutex.Lock()
	ControlConnections["data"] = conn
	admin.SaveControlConnection(conn)
	mutex.Unlock()

	for {
		message, err := proto.ReceiveMessage(conn)
		log.Println("Received Message", message)
		if err != nil {
			if err.Error() == "EOF" {
				panic("Server closed control connection stopping client now")
			}
			log.Println("Error on control connection " + err.Error())
		}
		if message.MessageType == "init-request" {
			tunnel := createNewTunnel(message)
			log.Println("Created a new Tunnel", message)
			localConn := createLocalConnection()
			log.Println("Created Local Connection", localConn.RemoteAddr())
			go func() {
				_, err := io.Copy(localConn, tunnel)
				if err != nil {
					log.Println("Error writing data to local connection ", err.Error())
				}
			}()
			log.Println("Writing data to local Connection")
			_, err := io.Copy(tunnel, localConn)
			if err != nil {
				log.Println("Error writing data to local connection ", err.Error())
			}

			log.Println("Finished Writing data to tunnel")
			tunnel.Close()
		}
		if message.MessageType == "ack-tunnel-create" {
			log.Println("Received Ack for creating tunnel from the upstream server")
			port, _ := strconv.Atoi(string(message.Data))
			admin.UpdateHostNameToPortMap(message.HostName, port)
		}
	}
}
