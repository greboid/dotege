package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/lego"
	"gopkg.in/yaml.v2"
)

const (
	envCertDestinationKey           = "DOTEGE_CERT_DESTINATION"
	envCertDestinationDefault       = "/data/certs/"
	envCertUserIdKey                = "DOTEGE_CERT_UID"
	envCertUserIdDefault            = -1
	envCertGroupIdKey               = "DOTEGE_CERT_GID"
	envCertGroupIdDefault           = -1
	envCertModeKey                  = "DOTEGE_CERT_MODE"
	envCertModeDefault              = 0600
	envDebugKey                     = "DOTEGE_DEBUG"
	envDebugContainersValue         = "containers"
	envDebugHeadersValue            = "headers"
	envDebugHostnamesValue          = "hostnames"
	envDnsProviderKey               = "DOTEGE_DNS_PROVIDER"
	envAcmeEmailKey                 = "DOTEGE_ACME_EMAIL"
	envAcmeEndpointKey              = "DOTEGE_ACME_ENDPOINT"
	envAcmeKeyTypeKey               = "DOTEGE_ACME_KEY_TYPE"
	envAcmeKeyTypeDefault           = "P384"
	envAcmeCacheLocationKey         = "DOTEGE_ACME_CACHE_FILE"
	envAcmeCacheLocationDefault     = "/data/config/certs.json"
	envSignalContainerKey           = "DOTEGE_SIGNAL_CONTAINER"
	envSignalContainerDefault       = ""
	envSignalTypeKey                = "DOTEGE_SIGNAL_TYPE"
	envSignalTypeDefault            = "HUP"
	envTemplateDestinationKey       = "DOTEGE_TEMPLATE_DESTINATION"
	envTemplateDestinationDefault   = "/data/output/haproxy.cfg"
	envTemplateSourceKey            = "DOTEGE_TEMPLATE_SOURCE"
	envTemplateSourceDefault        = "./templates/haproxy.cfg.tpl"
	envUsersKey                     = "DOTEGE_USERS"
	envUsersDefault                 = ""
	envWildcardDomainsKey           = "DOTEGE_WILDCARD_DOMAINS"
	envWildcardDomainsDefault       = ""
	envProxyTagKey                  = "DOTEGE_PROXYTAG"
	envProxyTagDefault              = ""
	envCertificateDeploymentKey     = "DOTEGE_CERTIFICATE_DEPLOYMENT"
	envCertificateDeploymentDefault = CertificateDeploymentCombined
)

const (
	CertificateDeploymentCombined = "combined"
	CertificateDeploymentSplit    = "splitkeys"
	CertificateDeploymentDisabled = "disabled"
)

// Config is the user-definable configuration for Dotege.
type Config struct {
	Templates              []TemplateConfig
	Signals                []ContainerSignal
	DefaultCertDestination string
	CertUid                int
	CertGid                int
	CertMode               os.FileMode
	Acme                   AcmeConfig
	WildCardDomains        []string
	Users                  []User
	ProxyTag               string
	CertificateDeployment  string

	DebugContainers bool
	DebugHeaders    bool
	DebugHostnames  bool
}

// User holds the details of a single user used for ACL purposes.
type User struct {
	Name     string   `yaml:"name"`
	Password string   `yaml:"password"`
	Groups   []string `yaml:"groups"`
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

func requiredStringVar(key string) (value string) {
	value, ok := os.LookupEnv(key)
	if !ok {
		panic(fmt.Errorf("required environmental variable not defined: %s", key))
	}
	return
}

func optionalStringVar(key string, fallback string) (value string) {
	value, ok := os.LookupEnv(key)
	if !ok {
		value = fallback
	}
	return
}

func optionalIntVar(key string, fallback int) int {
	if value, ok := os.LookupEnv(key); ok {
		if num, err := strconv.Atoi(value); err == nil {
			return num
		}
	}
	return fallback
}

func optionalFilemodeVar(key string, fallback os.FileMode) os.FileMode {
	if value, ok := os.LookupEnv(key); ok {
		if num, err := strconv.ParseInt(value, 8, 64); err == nil {
			return os.FileMode(num)
		}
	}
	return fallback
}

func createSignalConfig() []ContainerSignal {
	name := optionalStringVar(envSignalContainerKey, envSignalContainerDefault)
	if name == envSignalContainerDefault {
		return []ContainerSignal{}
	} else {
		return []ContainerSignal{
			{
				Name:   name,
				Signal: optionalStringVar(envSignalTypeKey, envSignalTypeDefault),
			},
		}
	}
}

func createConfig() *Config {
	debug := toMap(splitList(strings.ToLower(optionalStringVar(envDebugKey, ""))))
	c := &Config{
		Templates: []TemplateConfig{
			{
				Source:      optionalStringVar(envTemplateSourceKey, envTemplateSourceDefault),
				Destination: optionalStringVar(envTemplateDestinationKey, envTemplateDestinationDefault),
			},
		},
		Signals:                createSignalConfig(),
		DefaultCertDestination: optionalStringVar(envCertDestinationKey, envCertDestinationDefault),
		CertGid:                optionalIntVar(envCertGroupIdKey, envCertGroupIdDefault),
		CertUid:                optionalIntVar(envCertUserIdKey, envCertUserIdDefault),
		CertMode:               optionalFilemodeVar(envCertModeKey, envCertModeDefault),
		WildCardDomains:        splitList(optionalStringVar(envWildcardDomainsKey, envWildcardDomainsDefault)),
		Users:                  readUsers(),
		ProxyTag:               optionalStringVar(envProxyTagKey, envProxyTagDefault),
		CertificateDeployment:  optionalStringVar(envCertificateDeploymentKey, envCertificateDeploymentDefault),

		DebugContainers: debug[envDebugContainersValue],
		DebugHeaders:    debug[envDebugHeadersValue],
		DebugHostnames:  debug[envDebugHostnamesValue],
	}

	if c.CertificateDeployment != CertificateDeploymentDisabled {
		c.Acme = AcmeConfig{
			DnsProvider:   requiredStringVar(envDnsProviderKey),
			Email:         requiredStringVar(envAcmeEmailKey),
			Endpoint:      optionalStringVar(envAcmeEndpointKey, lego.LEDirectoryProduction),
			KeyType:       certcrypto.KeyType(optionalStringVar(envAcmeKeyTypeKey, envAcmeKeyTypeDefault)),
			CacheLocation: optionalStringVar(envAcmeCacheLocationKey, envAcmeCacheLocationDefault),
		}
	}

	return c
}

func readUsers() []User {
	var users []User
	err := yaml.Unmarshal([]byte(optionalStringVar(envUsersKey, envUsersDefault)), &users)
	if err != nil {
		panic(fmt.Errorf("unable to parse users struct: %s", err))
	}
	return users
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
