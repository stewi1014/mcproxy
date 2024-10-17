package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/stewi1014/mcproxy/protocol"
)

type ProxyConfig struct {
	Domain               string
	Destination_Ip       string
	Destination_Port     int
	Destination_Protocol int
	Destination_Version  string

	Shutdown_Timeout int

	Ec2_Server *EC2Config
}

func NewProxy(config ProxyConfig) (*Proxy, error) {
	var proxy Proxy

	proxy.domain = config.Domain
	proxy.destination_ip = config.Destination_Ip
	proxy.destination_port = config.Destination_Port
	proxy.status.protocolVersion = config.Destination_Protocol
	proxy.status.versionName = config.Destination_Version
	proxy.shutdown_timeout = time.Duration(config.Shutdown_Timeout) * time.Second

	if config.Ec2_Server != nil {
		server, err := NewEC2Server(*config.Ec2_Server)
		if err != nil {
			return nil, err
		}
		proxy.server = server
	} else {
		return nil, errors.New("no backend provided for proxy")
	}

	go func() {
		status, mcErr := proxy.getServerStatus()
		if mcErr == nil {
			log.Printf(
				"%v is running version %v: \"%v\"\n",
				proxy.domain,
				status.JSONResponse.Version.Name,
				status.JSONResponse.Description,
			)
			proxy.stopServer()
		} else {
			log.Printf("%v is stopped\n", proxy.domain)
		}

	}()
	return &proxy, nil
}

type Proxy struct {
	domain           string
	destination_ip   string
	destination_port int
	shutdown_timeout time.Duration
	connected        atomic.Int32

	server Server

	mutex             sync.Mutex
	lastStart         time.Time
	lastStartDuration time.Duration

	status struct {
		maxPlayers        int
		description       string
		versionName       string
		protocolVersion   int
		favicon           string
		enforceSecureChat bool
	}
}

func (p *Proxy) startServer() {
	p.mutex.Lock()
	log.Printf("starting server %v\n", p.domain)
	err := p.server.StartServer()
	if err != nil {
		p.mutex.Unlock()
		log.Println(err)
		return
	}

	if !p.lastStart.IsZero() {
		p.mutex.Unlock()
		return
	}
	p.lastStart = time.Now()
	defer func() {
		p.mutex.Lock()
		p.lastStart = time.Time{}
		p.mutex.Unlock()
	}()
	p.mutex.Unlock()

	for range time.NewTicker(time.Second).C {
		_, err := p.getServerStatus()
		if err == nil {
			p.mutex.Lock()
			p.lastStartDuration = time.Now().Sub(p.lastStart)
			p.mutex.Unlock()
			go p.stopServer()
			return
		}

		if time.Now().Sub(p.lastStart) > time.Minute*10 {
			log.Printf("server start timed out %+v\n", p)
			return
		}
	}

	panic("not reached")
}

func (p *Proxy) stopServer() {
	lastSuccess, lastPlayer := time.Now(), time.Now()

	for {
		if lastSuccess.Add(p.shutdown_timeout).Before(time.Now()) {
			log.Printf("server has frozen, trying to stop it; %+v\n", p)
			err := p.server.StopServer()
			if err != nil {
				log.Println(err)
			}
			return
		}

		if lastPlayer.Add(p.shutdown_timeout).Before(time.Now()) && lastSuccess.After(lastPlayer) {
			log.Printf("stopping server %v\n", p.domain)
			err := p.server.StopServer()
			if err != nil {
				log.Println(err)
			}
			return
		}

		time.Sleep(p.shutdown_timeout / 20)
		status, err := p.getServerStatus()
		if err != nil {
			continue
		}

		lastSuccess = time.Now()
		if status.JSONResponse.Players.Online > 0 {
			lastPlayer = time.Now()
		}
	}
}

func (p *Proxy) HandleStatus(conn *protocol.Conn) error {
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
			mcStatus, err := p.getServerStatus()
			if err == nil {
				err := conn.WritePacket(mcStatus)
				if err != nil {
					return err
				}
			} else {
				p.mutex.Lock()
				pStatus := p.status
				p.mutex.Unlock()

				var resp protocol.StatusResponse
				resp.JSONResponse.Players.Online = 0
				resp.JSONResponse.Players.Max = pStatus.maxPlayers
				resp.JSONResponse.Version.Name = pStatus.versionName
				resp.JSONResponse.Version.Protocol = pStatus.protocolVersion
				resp.JSONResponse.Favicon = pStatus.favicon
				resp.JSONResponse.EnforceSecureChat = pStatus.enforceSecureChat

				serverState, err := p.server.State()
				if err != nil {
					resp.JSONResponse.Players.Max = 0
					resp.JSONResponse.Description = "server is f**ked. sorry."
					err := conn.WritePacket(&resp)
					if err != nil {
						return err
					}
				} else {
					switch serverState {
					case ServerStateOff:
						resp.JSONResponse.Description = fmt.Sprintf("(sleeping) %v", pStatus.description)
					case ServerStateStarting:
						resp.JSONResponse.Description = fmt.Sprintf("(starting) %v", pStatus.description)
					case ServerStateOn:
						resp.JSONResponse.Description = pStatus.description
					case ServerStateStopping:
						resp.JSONResponse.Description = fmt.Sprintf("(stopping) %v", pStatus.description)
					}
				}

				err = conn.WritePacket(&resp)
				if err != nil {
					return err
				}
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

func (p *Proxy) HandleLogin(handshake *protocol.HandshakeIntention, mcconn *protocol.Conn) error {
	serverState, err := p.server.State()
	if err != nil {
		log.Println(err)
		_, mcErr := p.getServerStatus()
		if mcErr == nil {
			return p.pipeClient(handshake, mcconn)
		} else {
			var disconect protocol.Disconnect
			disconect.JSONTextComponent.Text = "Server is f**ked. Sorry choom."
			return mcconn.WritePacket(&disconect)
		}
	}

	switch serverState {
	case ServerStateOff:
		go p.startServer()
		var disconnect protocol.Disconnect
		p.mutex.Lock()
		lastStartDuration := p.lastStartDuration
		p.mutex.Unlock()

		time.Sleep(time.Second * 10)

		if lastStartDuration != 0 {
			disconnect.JSONTextComponent.Text = fmt.Sprintf(
				"Server is started. Should be up in approx %v",
				lastStartDuration,
			)
		} else {
			disconnect.JSONTextComponent.Text = "Server has been started"
		}
		return mcconn.WritePacket(&disconnect)

	case ServerStateStarting:
		start := time.Now()
		t := time.NewTicker(time.Second)
		for now := range t.C {
			_, err := p.getServerStatus()
			if err == nil {
				return p.pipeClient(handshake, mcconn)
			}

			if start.Add(time.Second * 10).Before(now) {
				break
			}
		}

		var disconect protocol.Disconnect
		p.mutex.Lock()
		lastStartDuration := p.lastStartDuration
		lastStart := p.lastStart
		p.mutex.Unlock()
		if !lastStart.IsZero() && lastStartDuration != 0 {
			disconect.JSONTextComponent.Text = fmt.Sprintf(
				"Server is starting. Should be up in approx %v",
				lastStart.Add(lastStartDuration).Sub(time.Now()),
			)
		} else {
			disconect.JSONTextComponent.Text = "Server is starting"
		}
		return mcconn.WritePacket(&disconect)

	case ServerStateOn:
		for range 10 {
			_, err := p.getServerStatus()
			if err == nil {
				return p.pipeClient(handshake, mcconn)
			}
			time.Sleep(time.Second)
		}
		return p.pipeClient(handshake, mcconn)

	case ServerStateStopping:
		var disconect protocol.Disconnect
		disconect.JSONTextComponent.Text = "Try again soon (server is halfway through shutting down)"
		return mcconn.WritePacket(&disconect)
	}

	var disconect protocol.Disconnect
	disconect.JSONTextComponent.Text = "This should never happen"
	return mcconn.WritePacket(&disconect)
}

func (p *Proxy) pipeClient(handshake *protocol.HandshakeIntention, mcconn *protocol.Conn) error {
	conn, err := net.Dial("tcp", fmt.Sprintf("%v:%v", p.destination_ip, p.destination_port))
	if err != nil {
		return err
	}

	p.connected.Add(1)
	defer p.connected.Add(-1)
	return mcconn.PipeTo(context.Background(), handshake, conn)
}

func (p *Proxy) getServerStatus() (*protocol.StatusResponse, error) {
	conn, err := net.DialTimeout(
		"tcp",
		fmt.Sprintf("%v:%v", p.destination_ip, p.destination_port),
		time.Second*2,
	)
	if err != nil {
		return &protocol.StatusResponse{}, err
	}
	defer conn.Close()

	err = conn.SetDeadline(time.Now().Add(time.Second * 3))
	if err != nil {
		return &protocol.StatusResponse{}, err
	}

	p.mutex.Lock()
	protocolVersion := p.status.protocolVersion
	p.mutex.Unlock()

	mcconn := protocol.NewConn(conn)
	err = mcconn.WritePacket(&protocol.HandshakeIntention{
		ProtocolVersion: protocol.VarInt(protocolVersion),
		ServerAddress:   protocol.String255(p.destination_ip),
		ServerPort:      protocol.UShort(p.destination_port),
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

	p.mutex.Lock()
	p.status.description = statusResponse.JSONResponse.Description
	p.status.enforceSecureChat = statusResponse.JSONResponse.EnforceSecureChat
	p.status.favicon = statusResponse.JSONResponse.Favicon
	p.status.maxPlayers = statusResponse.JSONResponse.Players.Max
	p.status.protocolVersion = statusResponse.JSONResponse.Version.Protocol
	p.status.versionName = statusResponse.JSONResponse.Version.Name
	p.mutex.Unlock()

	return statusResponse, err
}
