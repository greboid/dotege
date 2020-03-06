package main

import "strconv"

const (
	labelVhost = "com.chameth.vhost"
	labelProxy = "com.chameth.proxy"
	labelAuth  = "com.chameth.auth"
)

// Container describes a docker container that is running on the system.
type Container struct {
	Id     string
	Name   string
	Labels map[string]string
}

// ShouldProxy determines whether the container should be proxied to
func (c *Container) ShouldProxy() bool {
	_, hasVhost := c.Labels[labelVhost]
	hasPort := c.Port() > -1
	return hasPort && hasVhost
}

// Port returns the port the container accepts traffic on, or -1 if it could not be determined
func (c *Container) Port() int {
	l, ok := c.Labels[labelProxy]
	if ok {
		p, err := strconv.Atoi(l)

		if err != nil {
			logger.Warnf("Invalid port specification on container %s: %s (%v)", c.Name, l, err)
			return -1
		}

		if  p < 1 || p >= 1<<16 {
			logger.Warnf("Invalid port specification on container %s: %s (out of range)", c.Name, l)
			return -1
		}

		return p
	}
	return -1
}
