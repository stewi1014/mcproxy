package main

import "fmt"

type Server interface {
	StartServer() error
	StopServer() error
}

func NewDummyServer(name string) Server {
	return dummyServer(name)
}

type dummyServer string

func (l dummyServer) StartServer() error {
	fmt.Println("start server", l)
	return nil
}

func (l dummyServer) StopServer() error {
	fmt.Println("start server", l)
	return nil
}
