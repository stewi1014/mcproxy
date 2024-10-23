package main

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

const (
	ServerStateOff      = 1
	ServerStateStarting = 2
	ServerStateOn       = 3
	ServerStateStopping = 4
)

type Server interface {
	State(ctx context.Context) (int, error)
	StartServer(ctx context.Context) error
	StopServer(ctx context.Context) error
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

func (e *ec2Server) State(ctx context.Context) (int, error) {
	includeAll := true
	client := ec2.NewFromConfig(e.aws_config)
	out, err := client.DescribeInstanceStatus(ctx, &ec2.DescribeInstanceStatusInput{
		IncludeAllInstances: &includeAll,
		InstanceIds:         []string{e.instance_id},
	})
	if err != nil {
		return 0, err
	}

	if len(out.InstanceStatuses) == 0 {
		return 0, fmt.Errorf("unexpected nil in AWS InstanceStatuses")
	}

	if out.InstanceStatuses[0].InstanceState == nil {
		return 0, fmt.Errorf("unexpected nil in AWS InstanceStatuses[0].InstanceState")
	}

	if out.InstanceStatuses[0].InstanceState.Code == nil {
		return 0, fmt.Errorf("unexpected nil in AWS InstanceStatuses[0].InstanceState.Code")
	}

	switch (*out.InstanceStatuses[0].InstanceState.Code & 0xFF) / 16 {
	case 0:
		return ServerStateStarting, nil
	case 1:
		return ServerStateOn, nil
	case 2, 4:
		return ServerStateStopping, nil
	case 3, 5:
		return ServerStateOff, nil
	default:
		return 0, fmt.Errorf(
			"unknown instance state returned from AWS %v",
			*out.InstanceStatuses[0].InstanceState.Code,
		)
	}
}

func (e *ec2Server) StartServer(ctx context.Context) error {
	client := ec2.NewFromConfig(e.aws_config)
	_, err := client.StartInstances(ctx, &ec2.StartInstancesInput{
		InstanceIds: []string{e.instance_id},
	})
	if err != nil {
		return err
	}

	return nil
}

func (e *ec2Server) StopServer(ctx context.Context) error {
	client := ec2.NewFromConfig(e.aws_config)

	_, err := client.StopInstances(ctx, &ec2.StopInstancesInput{
		InstanceIds: []string{e.instance_id},
		Hibernate:   &e.hibernate,
	})
	if err != nil {
		return err
	}

	return nil
}
