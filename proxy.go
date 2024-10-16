package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/stewi1014/mcproxy/protocol"
)

type ProxyConfig struct {
	Domain               string
	Destination_Ip       string
	Destination_Port     int
	Destination_Protocol int
	Destination_Version  string

	Dummy_Server string
}

func NewProxy(config ProxyConfig) (*Proxy, error) {
	proxy := &Proxy{
		config: config,
	}

	if config.Dummy_Server != "" {
		proxy.server = NewDummyServer(config.Dummy_Server)
	} else {
		return nil, fmt.Errorf("no backend given for %v", config.Domain)
	}

	go proxy.handleServer()

	return proxy, nil
}

const (
	serverStateStopped  = 0
	serverStartStarted  = 1
	serverStateStarting = 2
	serverStateStopping = 3
	serverStateFucked   = 4
)

type Proxy struct {
	config           ProxyConfig
	serverState      int
	serverEstStarted time.Time
	startServerChan  chan struct{}
	ctx              context.Context
	server           Server

	hasServerInfo   bool
	description     string
	maxPlayers      int
	protocolVersion int
	mcVersion       string
}

func (p *Proxy) handleServer() {
	var lastStartDuration time.Duration
	p.startServerChan = make(chan struct{})

RESET:
	for {
		status, err := p.getServerStatus()
		if err == nil {
			p.hasServerInfo = true
			p.serverState = serverStartStarted
			p.description = status.JSONResponse.Description
			p.maxPlayers = status.JSONResponse.Players.Max
			p.protocolVersion = status.JSONResponse.Version.Protocol
			p.mcVersion = status.JSONResponse.Version.Name
		}

		if p.serverState == serverStartStarted {
			t := time.NewTicker(time.Second * 10)
			playerLastOnline := time.Now()
			for range t.C {
				status, err := p.getServerStatus()
				if err != nil {
					log.Println(err)
					p.serverState = serverStateStopped
					goto RESET
				}

				if status.JSONResponse.Players.Online > 0 {
					playerLastOnline = time.Now()
				}

				if playerLastOnline.Add(time.Minute * 2).Before(time.Now()) {
					err := p.server.StopServer()
					if err != nil {
						log.Println(err)
					}
				}
			}
		} else {
			<-p.startServerChan
			err := p.server.StartServer()
			if err != nil {
				log.Println(err)
				goto RESET
			}

			startTime := time.Now()
			if lastStartDuration != 0 {
				p.serverEstStarted = startTime.Add(lastStartDuration)
			}

			t := time.NewTicker(time.Second * 10)
			for range t.C {
				_, err := p.getServerStatus()
				if err != nil {
					if time.Now().Sub(startTime) > time.Minute*5 {
						p.serverState = serverStateFucked
						goto RESET
					}

					continue
				}

				p.serverState = serverStartStarted
			}
		}
	}
}

func (p *Proxy) handleStatus(conn *protocol.Conn) error {
	for {
		packet, err := conn.ReadPacket(
			&protocol.StatusRequest{},
			&protocol.PingRequest{},
		)

		if err != nil {
			return err
		}

		switch packet := packet.(type) {
		case *protocol.StatusRequest:
			err := conn.WritePacket(p.getStatus())
			if err != nil {
				return err
			}
		case *protocol.PingRequest:
			var pongResponse protocol.PongResponse
			pongResponse.Payload = packet.Payload
			err := conn.WritePacket(&pongResponse)
			if err != nil {
				return err
			}
		}
	}
}

func (p *Proxy) getStatusMessage() string {
	switch p.serverState {
	case serverStateStopped:
		return "server is sleeping"
	case serverStateStarting:
		if p.serverEstStarted.IsZero() {
			return "server is starting"
		}
		return fmt.Sprintf("server starting (approx %v remaining)", p.serverEstStarted.Sub(time.Now()))
	case serverStartStarted:
		return "server is running"
	case serverStateStopping:
		return "server is going to sleep"
	case serverStateFucked:
		return "server is f**ked"
	default:
		return "invalid server state"
	}
}

func (p *Proxy) handleLogin(handshake *protocol.HandshakeIntention, mcconn *protocol.Conn) error {
	if p.serverState == serverStartStarted || p.serverState == serverStateStarting {
		conn, err := net.Dial("tcp", fmt.Sprintf("%v:%v", p.config.Destination_Ip, p.config.Destination_Port))
		if err != nil {
			return err
		}

		return mcconn.PipeTo(context.Background(), handshake, conn)
	}

	if p.serverState == serverStateStopped {
		p.serverState = serverStateStarting
		p.startServerChan <- struct{}{}
	}

	time.Sleep(1)
	var disconnect protocol.Disconnect
	disconnect.JSONTextComponent.Text = p.getStatusMessage()
	return mcconn.WritePacket(&disconnect)
}

func (p *Proxy) getStatus() *protocol.StatusResponse {
	status, err := p.getServerStatus()
	if err == nil {
		return status
	}

	var statusResponse protocol.StatusResponse
	statusResponse.JSONResponse.Description = p.getStatusMessage()
	statusResponse.JSONResponse.Players.Online = 0

	if p.hasServerInfo {
		statusResponse.JSONResponse.Players.Max = p.maxPlayers
		statusResponse.JSONResponse.Version.Name = p.mcVersion
		statusResponse.JSONResponse.Version.Protocol = p.protocolVersion
	} else {
		statusResponse.JSONResponse.Players.Max = -1
		statusResponse.JSONResponse.Version.Name = p.config.Destination_Version
		statusResponse.JSONResponse.Version.Protocol = p.config.Destination_Protocol
	}

	return &statusResponse
}

func (p *Proxy) getServerStatus() (*protocol.StatusResponse, error) {
	conn, err := net.Dial("tcp", fmt.Sprintf("%v:%v", p.config.Destination_Ip, p.config.Destination_Port))
	if err != nil {
		return &protocol.StatusResponse{}, err
	}
	defer conn.Close()

	err = conn.SetDeadline(time.Now().Add(time.Second * 10))
	if err != nil {
		return &protocol.StatusResponse{}, err
	}

	mcconn := protocol.NewConn(conn)
	err = mcconn.WritePacket(&protocol.HandshakeIntention{
		ProtocolVersion: protocol.VarInt(p.config.Destination_Protocol),
		ServerAddress:   protocol.String255(p.config.Destination_Ip),
		ServerPort:      protocol.UShort(p.config.Destination_Port),
		NextState:       protocol.StateStatus,
	})
	if err != nil {
		return &protocol.StatusResponse{}, err
	}

	err = mcconn.WritePacket(&protocol.StatusRequest{})
	if err != nil {
		return &protocol.StatusResponse{}, err
	}

	resposePacket, err := mcconn.ReadPacket(&protocol.StatusResponse{})
	if err != nil {
		return &protocol.StatusResponse{}, err
	}

	statusResponse := resposePacket.(*protocol.StatusResponse)
	return statusResponse, err
}
