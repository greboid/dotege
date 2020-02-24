package main

import (
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"golang.org/x/net/context"
)

type DockerClient interface {
	Events(ctx context.Context, options types.EventsOptions) (<-chan events.Message, <-chan error)
	ContainerList(ctx context.Context, options types.ContainerListOptions) ([]types.Container, error)
	ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error)
}

type ContainerMonitor struct {
	client DockerClient
}

type Operation int

const (
	Added = iota
	Removed
)

type ContainerEvent struct {
	Operation Operation
	Container Container
}

func (m ContainerMonitor) monitor(ctx context.Context, output chan<- ContainerEvent) error {
	ctx, cancel := context.WithCancel(ctx)
	stream, errors := m.startEventStream(ctx)

	if err := m.publishExistingContainers(ctx, output); err != nil {
		cancel()
		return err
	}

	for {
		select {
		case event := <-stream:
			if event.Action == "create" {
				err, container := m.inspectContainer(ctx, event.Actor.ID)
				if err != nil {
					cancel()
					return err
				}
				output <- ContainerEvent{
					Operation: Added,
					Container: container,
				}
			} else {
				output <- ContainerEvent{
					Operation: Removed,
					Container: Container{
						Name: event.Actor.Attributes["name"],
					},
				}
			}

		case err := <-errors:
			cancel()
			return err

		case <-ctx.Done():
			return nil
		}
	}
}

func (m ContainerMonitor) startEventStream(ctx context.Context) (<-chan events.Message, <-chan error) {
	args := filters.NewArgs()
	args.Add("type", "container")
	args.Add("event", "create")
	args.Add("event", "destroy")
	return m.client.Events(ctx, types.EventsOptions{Filters: args})
}

func (m ContainerMonitor) publishExistingContainers(ctx context.Context, output chan<- ContainerEvent) error {
	containers, err := m.client.ContainerList(ctx, types.ContainerListOptions{})
	if err != nil {
		return fmt.Errorf("unable to list containers: %s", err.Error())
	}

	for _, container := range containers {
		output <- ContainerEvent{
			Operation: Added,
			Container: Container{
				Id:     container.ID,
				Name:   container.Names[0][1:],
				Labels: container.Labels,
			},
		}
	}
	return nil
}

func (m ContainerMonitor) inspectContainer(ctx context.Context, id string) (error, Container) {
	container, err := m.client.ContainerInspect(ctx, id)
	if err != nil {
		return err, Container{}
	}

	return nil, Container{
		Id:     container.ID,
		Name:   container.Name[1:],
		Labels: container.Config.Labels,
	}
}
