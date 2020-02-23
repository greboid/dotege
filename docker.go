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

func monitorContainers(client DockerClient, ctx context.Context, addedFn func(*Container), removedFn func(string)) error {
	ctx, cancel := context.WithCancel(ctx)
	stream, errors := startEventStream(client, ctx)

	if err := publishExistingContainers(client, ctx, addedFn); err != nil {
		cancel()
		return err
	}

	for {
		select {
		case event := <-stream:
			if event.Action == "create" {
				err, container := inspectContainer(client, ctx, event.Actor.ID)
				if err != nil {
					cancel()
					return err
				}
				addedFn(&container)
			} else {
				removedFn(event.Actor.Attributes["name"])
			}

		case err := <-errors:
			cancel()
			return err

		case <-ctx.Done():
			return nil
		}
	}
}

func startEventStream(client DockerClient, ctx context.Context) (<-chan events.Message, <-chan error) {
	args := filters.NewArgs()
	args.Add("type", "container")
	args.Add("event", "create")
	args.Add("event", "destroy")
	return client.Events(ctx, types.EventsOptions{Filters: args})
}

func publishExistingContainers(client DockerClient, ctx context.Context, addedFn func(*Container)) error {
	containers, err := client.ContainerList(ctx, types.ContainerListOptions{})
	if err != nil {
		return fmt.Errorf("unable to list containers: %s", err.Error())
	}

	for _, container := range containers {
		addedFn(&Container{
			Id:     container.ID,
			Name:   container.Names[0][1:],
			Labels: container.Labels,
		})
	}
	return nil
}

func inspectContainer(client DockerClient, ctx context.Context, id string) (error, Container) {
	container, err := client.ContainerInspect(ctx, id)
	if err != nil {
		return err, Container{}
	}

	return nil, Container{
		Id:     container.ID,
		Name:   container.Name[1:],
		Labels: container.Config.Labels,
	}
}
