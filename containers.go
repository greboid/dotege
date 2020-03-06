package main

import (
	"strconv"
	"strings"
)

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

		if p < 1 || p >= 1<<16 {
			logger.Warnf("Invalid port specification on container %s: %s (out of range)", c.Name, l)
			return -1
		}

		return p
	}
	return -1
}

// CertNames returns a list of names required on a certificate for this container, taking into account wildcard
// configuration.
func (c *Container) CertNames() []string {
	if label, ok := c.Labels[labelVhost]; ok {
		return applyWildcards(splitList(label), config.WildCardDomains)
	} else {
		return []string{}
	}
}

// applyWildcards replaces domains with matching wildcards
func applyWildcards(domains []string, wildcards []string) (result []string) {
	result = []string{}
	required := make(map[string]bool)
	for _, domain := range domains {
		found := false
		for _, wildcard := range wildcards {
			if wildcardMatches(wildcard, domain) {
				if !required["*."+wildcard] {
					result = append(result, "*."+wildcard)
					required["*."+wildcard] = true
				}
				found = true
				break
			}
		}

		if !found && !required[domain] {
			result = append(result, domain)
			required[domain] = true
		}
	}
	return
}

// wildcardMatches tests if the given wildcard matches the domain
func wildcardMatches(wildcard, domain string) bool {
	if len(domain) <= len(wildcard) {
		return false
	}

	pivot := len(domain) - len(wildcard) - 1
	start := domain[:pivot]
	end := domain[pivot+1:]
	return domain[pivot] == '.' && end == wildcard && !strings.ContainsRune(start, '.')
}

// Containers maps container IDs to their corresponding information
type Containers map[string]*Container

// TemplateContext builds a context to use to render templates
func (c Containers) TemplateContext() TemplateContext {
	return TemplateContext{
		Containers: c,
		Hostnames:  c.hostnames(),
	}
}

// hostnames builds a mapping of primary hostnames to deals about the containers that use them
func (c Containers) hostnames() (hostnames map[string]*Hostname) {
	hostnames = make(map[string]*Hostname)
	for _, container := range c {
		if label, ok := container.Labels[labelVhost]; ok {
			names := splitList(label)
			primary := names[0]

			h := hostnames[primary]
			if h == nil {
				h = NewHostname(primary)
				hostnames[primary] = h
			}

			h.update(names[1:], container)
		}
	}
	return
}

// Hostname describes a DNS name used for proxying, retrieving certificates, etc.
type Hostname struct {
	Name         string
	Alternatives map[string]string
	Containers   []*Container
	RequiresAuth bool
	AuthGroup    string
}

// NewHostname creates a new hostname with the given name
func NewHostname(name string) *Hostname {
	return &Hostname{
		Name:         name,
		Alternatives: make(map[string]string),
	}
}

// update adds the alternate names and container information to the hostname
func (h *Hostname) update(alternates []string, container *Container) {
	h.Containers = append(h.Containers, container)

	for _, a := range alternates {
		h.Alternatives[a] = a
	}

	if label, ok := container.Labels[labelAuth]; ok {
		h.RequiresAuth = true
		h.AuthGroup = label
	}
}
