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
	Domains              []string
	Destination_Ip       string
	Destination_Port     int
	Destination_Protocol int
	Destination_Version  string

	Shutdown_Timeout int

	Ec2_Server *EC2Config
	Logger     *log.Logger
}

func NewProxy(config ProxyConfig) (*Proxy, error) {
	var proxy Proxy

	proxy.log = config.Logger
	proxy.domains = config.Domains
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
		status, mcErr := proxy.getServerStatus(time.Second * 10)
		if mcErr == nil {
			proxy.log.Printf(
				"running version %v: \"%v\"\n",
				status.JSONResponse.Version.Name,
				status.JSONResponse.Description,
			)
			proxy.stopWhenEmpty()
		} else {
			log.Printf("%v is stopped\n", proxy.domains)
		}
	}()
	return &proxy, nil
}

type Proxy struct {
	log *log.Logger

	domains          []string
	destination_ip   string
	destination_port int
	shutdown_timeout time.Duration
	connected        atomic.Int32

	server     Server
	hasStarted bool

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

func (p *Proxy) startServer() error {
	p.mutex.Lock()
	if p.hasStarted {
		p.mutex.Unlock()
		return nil
	}
	p.hasStarted = true
	p.lastStart = time.Now()
	p.mutex.Unlock()

	p.log.Println("starting server")

	err := p.server.StartServer(context.Background())
	if err != nil {
		return err
	}

	go func() {
		for now := range time.NewTicker(time.Second * 2).C {
			_, err := p.getServerStatus(time.Second * 10)
			if err == nil {
				p.log.Println("server has started")
				p.mutex.Lock()
				p.lastStartDuration = now.Sub(p.lastStart)
				p.mutex.Unlock()
				p.stopWhenEmpty()
				return
			}

			state, err := p.server.State(context.Background())
			if err != nil {
				p.log.Println("failed to get server state: ", err)
			} else {
				if state != ServerStateStarting && state != ServerStateOn {
					p.log.Println("server shutdown before mc server could be connected to")
					return
				}
			}
		}
	}()

	return nil
}

func (p *Proxy) stopWhenEmpty() {
	defer func() {
		p.mutex.Lock()
		p.hasStarted = false
		p.mutex.Unlock()
	}()

	lastPlayer := time.Now()
	for t := range time.NewTicker(p.shutdown_timeout / 10).C {
		state, stateErr := p.server.State(context.Background())
		if stateErr != nil {
			p.log.Println("failed to get server state: ", stateErr)
		} else if state != ServerStateStarting && state != ServerStateOn {
			p.log.Println("server shut down externally")
			return
		}

		status, statusErr := p.getServerStatus(time.Second * 10)
		if statusErr == nil {
			if status.JSONResponse.Players.Online > 0 {
				lastPlayer = t
			} else if lastPlayer.Add(p.shutdown_timeout).Before(t) {
				err := p.server.StopServer(context.Background())
				if err != nil {
					p.log.Println("error stopping server:", err)
				} else {
					p.log.Println("stopping server")
					return
				}
			}
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
			state, stateErr := p.server.State(context.Background())
			if stateErr != nil {
				p.log.Println("failed to read server state:", stateErr)

				status, statusErr := p.getServerStatus(time.Second * 10)
				if statusErr == nil {
					err := conn.WritePacket(status)
					if err != nil {
						return err
					}
				}
				return statusErr
			}

			p.mutex.Lock()
			pStatus := p.status
			lastStartDuration := p.lastStartDuration
			lastStart := p.lastStart
			p.mutex.Unlock()

			var resp protocol.StatusResponse
			resp.JSONResponse.Players.Online = 0
			resp.JSONResponse.Players.Max = pStatus.maxPlayers
			resp.JSONResponse.Version.Name = pStatus.versionName
			resp.JSONResponse.Version.Protocol = pStatus.protocolVersion
			resp.JSONResponse.Favicon = pStatus.favicon
			resp.JSONResponse.EnforceSecureChat = pStatus.enforceSecureChat

			switch state {
			case ServerStateOff:
				resp.JSONResponse.Description = fmt.Sprintf("(sleeping) %v", pStatus.description)

			case ServerStateStarting:
				if !lastStart.IsZero() && lastStartDuration != 0 {
					resp.JSONResponse.Description = fmt.Sprintf(
						"(starting approx %v) %v",
						lastStart.Add(lastStartDuration).Sub(time.Now()).Round(time.Second),
						pStatus.description,
					)
				} else {
					resp.JSONResponse.Description = fmt.Sprintf("(starting) %v", pStatus.description)
				}

			case ServerStateOn:
				status, err := p.getServerStatus(time.Second)
				if err == nil {
					resp = *status
				} else {
					if lastStart.Add(p.shutdown_timeout).Before(time.Now()) {
						resp.JSONResponse.Description = fmt.Sprintf("(not responding) %v", pStatus.description)
					} else {
						if !lastStart.IsZero() && lastStartDuration != 0 {
							resp.JSONResponse.Description = fmt.Sprintf(
								"(starting approx %v) %v",
								lastStart.Add(lastStartDuration).Sub(time.Now()).Round(time.Second),
								pStatus.description,
							)
						} else {
							resp.JSONResponse.Description = fmt.Sprintf("(starting) %v", pStatus.description)
						}
					}
				}

			case ServerStateStopping:
				resp.JSONResponse.Description = fmt.Sprintf("(stopping) %v", pStatus.description)
			}

			err = conn.WritePacket(&resp)
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

func (p *Proxy) HandleLogin(handshake *protocol.HandshakeIntention, mcconn *protocol.Conn, raddr net.Addr) error {
	p.log.Println("login from", raddr)
	serverState, err := p.server.State(context.Background())
	if err != nil {
		p.log.Println(err)
		_, mcErr := p.getServerStatus(time.Second * 10)
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
		var disconnect protocol.Disconnect
		err := p.startServer()
		if err != nil {
			disconnect.JSONTextComponent.Text = fmt.Sprintf("Failed to start server: %v", err)

		} else {
			time.Sleep(time.Second * 10)

			p.mutex.Lock()
			lastStart := p.lastStart
			lastStartDuration := p.lastStartDuration
			p.mutex.Unlock()

			if lastStartDuration != 0 {
				disconnect.JSONTextComponent.Text = fmt.Sprintf(
					"Server is started. Should be up in approx %v",
					lastStart.Add(lastStartDuration).Sub(time.Now()).Round(time.Second),
				)
			} else {
				disconnect.JSONTextComponent.Text = "Server has been started"
			}
		}
		return mcconn.WritePacket(&disconnect)

	case ServerStateStarting:
		start := time.Now()
		t := time.NewTicker(time.Second)
		for now := range t.C {
			_, err := p.getServerStatus(time.Second * 10)
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
			_, err := p.getServerStatus(time.Second * 10)
			if err == nil {
				return p.pipeClient(handshake, mcconn)
			}
			time.Sleep(time.Second * 2)
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

func (p *Proxy) getServerStatus(timeout time.Duration) (*protocol.StatusResponse, error) {
	conn, err := net.DialTimeout(
		"tcp",
		fmt.Sprintf("%v:%v", p.destination_ip, p.destination_port),
		timeout,
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

	go func() {
		p.mutex.Lock()
		p.status.description = statusResponse.JSONResponse.Description
		p.status.enforceSecureChat = statusResponse.JSONResponse.EnforceSecureChat
		p.status.favicon = statusResponse.JSONResponse.Favicon
		p.status.maxPlayers = statusResponse.JSONResponse.Players.Max
		p.status.protocolVersion = statusResponse.JSONResponse.Version.Protocol
		p.status.versionName = statusResponse.JSONResponse.Version.Name
		p.mutex.Unlock()
	}()

	return statusResponse, err
}
