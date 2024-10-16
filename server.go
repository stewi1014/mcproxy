package main

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

type Server interface {
	IsRunning() (bool, error)
	StartServer() error
	StopServer() error
}

func NewDummyServer(name string) Server {
	return dummyServer(name)
}

type dummyServer string

func (l dummyServer) IsRunning() (bool, error) {
	return true, nil
}

func (l dummyServer) StartServer() error {
	fmt.Println("start server", l)
	return nil
}

func (l dummyServer) StopServer() error {
	fmt.Println("start server", l)
	return nil
}

type EC2Config struct {
	Instance_Id string
	Region      string
	Hibernate   bool
}

func NewEC2Server(cfg EC2Config) (Server, error) {
	aws_config, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(cfg.Region))
	if err != nil {
		return nil, err
	}

	return &ec2Server{
		aws_config:  aws_config,
		instance_id: cfg.Instance_Id,
		hibernate:   cfg.Hibernate,
	}, nil
}

type ec2Server struct {
	aws_config  aws.Config
	instance_id string
	hibernate   bool
}

func (e *ec2Server) IsRunning() (bool, error) {
	includeAll := true
	client := ec2.NewFromConfig(e.aws_config)
	out, err := client.DescribeInstanceStatus(context.TODO(), &ec2.DescribeInstanceStatusInput{
		IncludeAllInstances: &includeAll,
		InstanceIds:         []string{e.instance_id},
	})
	if err != nil {
		return false, err
	}

	if len(out.InstanceStatuses) == 0 {
		return false, fmt.Errorf("unexpected nil in AWS InstanceStatuses")
	}

	if out.InstanceStatuses[0].InstanceState == nil {
		return false, fmt.Errorf("unexpected nil in AWS InstanceStatuses[0].InstanceState")
	}

	if out.InstanceStatuses[0].InstanceState.Code == nil {
		return false, fmt.Errorf("unexpected nil in AWS InstanceStatuses[0].InstanceState.Code")
	}

	return (*out.InstanceStatuses[0].InstanceState.Code & 0xFF) < 32, nil
}

func (e *ec2Server) StartServer() error {
	client := ec2.NewFromConfig(e.aws_config)
	_, err := client.StartInstances(context.TODO(), &ec2.StartInstancesInput{
		InstanceIds: []string{e.instance_id},
	})
	if err != nil {
		return err
	}

	return nil
}

func (e *ec2Server) StopServer() error {
	client := ec2.NewFromConfig(e.aws_config)

	_, err := client.StopInstances(context.TODO(), &ec2.StopInstancesInput{
		InstanceIds: []string{e.instance_id},
		Hibernate:   &e.hibernate,
	})
	if err != nil {
		return err
	}

	return nil
}
