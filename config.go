package main

import (
	"fmt"
	"github.com/go-acme/lego/v3/certcrypto"
	"github.com/go-acme/lego/v3/lego"
	"os"
	"strings"
)

const (
	envCertDestinationKey         = "DOTEGE_CERT_DESTINATION"
	envCertDestinationDefault     = "/data/certs/"
	envDebugKey                   = "DOTEGE_DEBUG"
	envDebugHeadersValue          = "headers"
	envDnsProviderKey             = "DOTEGE_DNS_PROVIDER"
	envAcmeEmailKey               = "DOTEGE_ACME_EMAIL"
	envAcmeEndpointKey            = "DOTEGE_ACME_ENDPOINT"
	envAcmeKeyTypeKey             = "DOTEGE_ACME_KEY_TYPE"
	envAcmeKeyTypeDefault         = "P384"
	envAcmeCacheLocationKey       = "DOTEGE_ACME_CACHE_FILE"
	envAcmeCacheLocationDefault   = "/data/config/certs.json"
	envSignalContainerKey         = "DOTEGE_SIGNAL_CONTAINER"
	envSignalContainerDefault     = ""
	envSignalTypeKey              = "DOTEGE_SIGNAL_TYPE"
	envSignalTypeDefault          = "HUP"
	envTemplateDestinationKey     = "DOTEGE_TEMPLATE_DESTINATION"
	envTemplateDestinationDefault = "/data/output/haproxy.cfg"
	envTemplateSourceKey          = "DOTEGE_TEMPLATE_SOURCE"
	envTemplateSourceDefault      = "./templates/haproxy.cfg.tpl"
	envWildcardDomainsKey         = "DOTEGE_WILDCARD_DOMAINS"
	envWildcardDomainsDefault     = ""
)

// Config is the user-definable configuration for Dotege.
type Config struct {
	Templates              []TemplateConfig
	Signals                []ContainerSignal
	DefaultCertDestination string
	Acme                   AcmeConfig
	WildCardDomains        []string
	DebugHeaders           bool
}

// TemplateConfig configures a single template for the generator.
type TemplateConfig struct {
	Source      string
	Destination string
}

// ContainerSignal describes a container that should be sent a signal when the config/certs change.
type ContainerSignal struct {
	Name   string
	Signal string
}

// AcmeConfig describes the configuration to use for getting certs using ACME.
type AcmeConfig struct {
	Email         string
	DnsProvider   string
	Endpoint      string
	KeyType       certcrypto.KeyType
	CacheLocation string
}

func requiredVar(key string) (value string) {
	value, ok := os.LookupEnv(key)
	if !ok {
		panic(fmt.Errorf("required environmental variable not defined: %s", key))
	}
	return
}

func optionalVar(key string, fallback string) (value string) {
	value, ok := os.LookupEnv(key)
	if !ok {
		value = fallback
	}
	return
}

func createSignalConfig() []ContainerSignal {
	name := optionalVar(envSignalContainerKey, envSignalContainerDefault)
	if name == envSignalContainerDefault {
		return []ContainerSignal{}
	} else {
		return []ContainerSignal{
			{
				Name:   name,
				Signal: optionalVar(envSignalTypeKey, envSignalTypeDefault),
			},
		}
	}
}

func createConfig() *Config {
	debug := toMap(splitList(strings.ToLower(optionalVar(envDebugKey, ""))))
	return &Config{
		Templates: []TemplateConfig{
			{
				Source:      optionalVar(envTemplateSourceKey, envTemplateSourceDefault),
				Destination: optionalVar(envTemplateDestinationKey, envTemplateDestinationDefault),
			},
		},
		Acme: AcmeConfig{
			DnsProvider:   requiredVar(envDnsProviderKey),
			Email:         requiredVar(envAcmeEmailKey),
			Endpoint:      optionalVar(envAcmeEndpointKey, lego.LEDirectoryProduction),
			KeyType:       certcrypto.KeyType(optionalVar(envAcmeKeyTypeKey, envAcmeKeyTypeDefault)),
			CacheLocation: optionalVar(envAcmeCacheLocationKey, envAcmeCacheLocationDefault),
		},
		Signals:                createSignalConfig(),
		DefaultCertDestination: optionalVar(envCertDestinationKey, envCertDestinationDefault),
		WildCardDomains:        splitList(optionalVar(envWildcardDomainsKey, envWildcardDomainsDefault)),
		DebugHeaders:           debug[envDebugHeadersValue],
	}
}

func splitList(input string) (result []string) {
	result = []string{}
	for _, part := range strings.Split(strings.ReplaceAll(input, " ", ","), ",") {
		if len(part) > 0 {
			result = append(result, part)
		}
	}
	return
}

func toMap(input []string) map[string]bool {
	res := make(map[string]bool)
	for k := range input {
		res[input[k]] = true
	}
	return res
}
