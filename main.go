package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"

	"github.com/stewi1014/mcproxy/protocol"
)

type Config struct {
	Listen struct {
		Address           string
		Fallback_Version  string
		Fallback_Protocol int
	}
	Proxies []ProxyConfig
}

func openConfig() Config {
	var config Config

	configFile, err := os.Open("config.json")
	if err != nil {
		panic(err)
	}
	defer configFile.Close()

	dec := json.NewDecoder(configFile)
	err = dec.Decode(&config)
	if err != nil {
		panic(err)
	}

	return config
}

func main() {
	config := openConfig()

	proxies := make(map[string]*Proxy)

	for _, proxyConfig := range config.Proxies {
		proxy, err := NewProxy(proxyConfig)
		if err != nil {
			fmt.Println(proxyConfig.Domain, err)
			return
		}
		proxies[proxyConfig.Domain] = proxy
	}

	l, err := net.Listen("tcp", config.Listen.Address)
	if err != nil {
		fmt.Println(err)
		return
	}

	for {
		conn, err := l.Accept()
		if err != nil {
			return
		}

		go handleHandshake(config, conn, proxies)
	}
}

func handleHandshake(config Config, conn net.Conn, proxies map[string]*Proxy) {
	log := log.New(os.Stderr, conn.RemoteAddr().String()+" ", log.LstdFlags)

	defer conn.Close()
	mcconn := protocol.NewConn(conn)

	packet, err := mcconn.ReadPacket(&protocol.HandshakeIntention{})
	if err != nil {
		log.Println(err)
		return
	}

	handshake := packet.(*protocol.HandshakeIntention)
	proxy, ok := proxies[string(handshake.ServerAddress)]
	if ok {
		if handshake.NextState == protocol.StateStatus {
			err := proxy.HandleStatus(mcconn)
			if err != nil {
				if !errors.Is(err, io.EOF) {
					log.Println(err)
				}
				return
			}
		} else if handshake.NextState == protocol.StateLogin {
			err := proxy.HandleLogin(handshake, mcconn)
			if err != nil {
				if !errors.Is(err, io.EOF) {
					log.Println(err)
				}
				return
			}
		} else {
			log.Println("unknown state", handshake.NextState)
		}

		return
	}

	if handshake.NextState == protocol.StateStatus {
		for {
			packet, err := mcconn.ReadPacket(
				&protocol.StatusRequest{},
				&protocol.PingRequest{},
			)

			if err != nil {
				if !errors.Is(err, io.EOF) {
					log.Println(err)
				}
				return
			}

			switch packet := packet.(type) {
			case *protocol.StatusRequest:
				var statusResponse protocol.StatusResponse
				statusResponse.JSONResponse.Description = fmt.Sprintf("unknown server %v", handshake.ServerAddress)
				statusResponse.JSONResponse.Version.Name = config.Listen.Fallback_Version
				statusResponse.JSONResponse.Version.Protocol = config.Listen.Fallback_Protocol
				statusResponse.JSONResponse.Players.Online = -1
				statusResponse.JSONResponse.Players.Max = -1
				err := mcconn.WritePacket(&statusResponse)
				if err != nil {
					log.Println(err)
					return
				}
			case *protocol.PingRequest:
				var pongResponse protocol.PongResponse
				pongResponse.Payload = packet.Payload
				err := mcconn.WritePacket(&pongResponse)
				if err != nil {
					log.Println(err)
					return
				}
			}
		}
	}
}
