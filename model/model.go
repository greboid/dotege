package model

// Container models a docker container that is running on the system.
type Container struct {
	Id     string
	Name   string
	Labels map[string]string
}

// LabelConfig describes the labels used for various properties.
type LabelConfig struct {
	Hostnames string
}

// Hostname describes a DNS name used for proxying, retrieving certificates, etc.
type Hostname struct {
	Name         string
	Alternatives map[string]bool
	Containers   []Container
}

// Config is the user-definable configuration for Dotege.
type Config struct {
	Templates []TemplateConfig
	Labels    LabelConfig
}

// TemplateConfig configures a single template for the generator.
type TemplateConfig struct {
	Source      string
	Destination string
}
