package main

import (
	"context"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"go.uber.org/zap"
	"time"
)

type ContainerMonitor struct {
	logger             *zap.SugaredLogger
	newContainers      chan<- Container
	goneContainerNames chan<- string
	client             *client.Client
	expiryTimes        map[string]time.Time
	deletionTime       time.Duration
	nextExpiry         time.Time
	expiryTimer        *time.Timer
}

func NewContainerMonitor(logger *zap.SugaredLogger, client *client.Client, newContainerChannel chan<- Container, goneContainerChannel chan<- string) *ContainerMonitor {
	timer := time.NewTimer(time.Hour)
	timer.Stop()

	return &ContainerMonitor{
		logger:             logger,
		newContainers:      newContainerChannel,
		goneContainerNames: goneContainerChannel,
		client:             client,
		expiryTimes:        make(map[string]time.Time),
		deletionTime:       10 * time.Second,
		expiryTimer:        timer,
		nextExpiry:         time.Now(),
	}
}

func (c *ContainerMonitor) Monitor() {
	args := filters.NewArgs()
	args.Add("type", "container")
	args.Add("event", "create")
	args.Add("event", "destroy")
	eventsChan, errChan := c.client.Events(context.Background(), types.EventsOptions{Filters: args})

	c.publishExistingContainers()

	for {
		select {
		case event := <-eventsChan:
			if event.Action == "create" {
				c.publishNewContainer(event.Actor.ID)
			} else {
				c.scheduleExpiry(event.Actor.Attributes["name"])
			}

		case <-c.expiryTimer.C:
			c.publishExpiredContainers()

		case err := <-errChan:
			c.logger.Fatal("Error received from docker events API", err)
		}
	}
}

func (c *ContainerMonitor) publishExistingContainers() {
	containers, err := c.client.ContainerList(context.Background(), types.ContainerListOptions{})
	if err != nil {
		c.logger.Fatal("Error received trying to list containers", err)
	}

	for _, container := range containers {
		c.logger.Infof("Found existing container %s", container.Names[0][1:])
		c.newContainers <- Container{
			Id:     container.ID,
			Name:   container.Names[0][1:],
			Labels: container.Labels,
		}
	}
}

func (c *ContainerMonitor) publishNewContainer(id string) {
	container, err := c.client.ContainerInspect(context.Background(), id)
	if err != nil {
		c.logger.Fatal("Error received trying to inspect container", err)
	}
	c.newContainers <- Container{
		Id:     container.ID,
		Name:   container.Name[1:],
		Labels: container.Config.Labels,
	}
	c.logger.Info("Found new container %s", container.Name[1:])
	delete(c.expiryTimes, container.Name[1:])
}

func (c *ContainerMonitor) scheduleExpiry(name string) {
	now := time.Now()
	expiryTime := now.Add(c.deletionTime)
	c.expiryTimes[name] = expiryTime
	c.logger.Info("Scheduling expiry timer for %s", name)
	if c.nextExpiry.Before(now) || c.nextExpiry.After(expiryTime) {
		c.logger.Debug("Starting expiry timer with default duration")
		c.expiryTimer.Reset(c.deletionTime + 1*time.Second)
		c.nextExpiry = expiryTime
	}
}

func (c *ContainerMonitor) publishExpiredContainers() {
	now := time.Now()
	next := 0 * time.Second

	for name, expiryTime := range c.expiryTimes {
		if expiryTime.Before(now) {
			c.logger.Info("Expiring %s", name)
			delete(c.expiryTimes, name)
			c.goneContainerNames <- name
		} else if next == 0 || expiryTime.Sub(now) < next {
			next = expiryTime.Sub(now)
		}
	}

	if next > 0 {
		c.logger.Debugf("Starting expiry timer with duration %s\n", next)
		c.expiryTimer.Reset(next + 1*time.Second)
		c.nextExpiry = now.Add(next)
	}
}
