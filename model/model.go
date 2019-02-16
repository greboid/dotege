package model

import "time"

// CertActions define what will be done with a certificate
type CertActions uint8

// constants defining CertActions
const (
	// COMBINE the full chain and private key into one file
	COMBINE CertActions = 1 << iota
	// FLATTEN the directory structure so all files are in one dir
	FLATTEN
	// CHMOD the files so they are world readable (potentially dangerous!)
	CHMOD
)

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
	Name            string
	Alternatives    map[string]bool
	Containers      []Container
	CertActions     CertActions
	CertDestination string
}

// Config is the user-definable configuration for Dotege.
type Config struct {
	Templates              []TemplateConfig
	Labels                 LabelConfig
	DefaultCertActions     CertActions
	DefaultCertDestination string
}

// TemplateConfig configures a single template for the generator.
type TemplateConfig struct {
	Source      string
	Destination string
}

// FoundCertificate describes a certificate we've located on disk.
type FoundCertificate struct {
	Hostname   string
	Cert       string
	Chain      string
	FullChain  string
	PrivateKey string
	ModTime    time.Time
}
