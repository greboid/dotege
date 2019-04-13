package main

import (
	"bytes"
	"context"
	"fmt"
	"github.com/docker/docker/client"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"io/ioutil"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"
)

// Container models a docker container that is running on the system.
type Container struct {
	Id     string
	Name   string
	Labels map[string]string
}

// Hostname describes a DNS name used for proxying, retrieving certificates, etc.
type Hostname struct {
	Name            string
	Alternatives    map[string]bool
	Containers      []*Container
	CertDestination string
	RequiresAuth    bool
	AuthGroup       string
}

var (
	logger             *zap.SugaredLogger
	certificateManager *CertificateManager
	dockerClient       *client.Client
	config             *Config
	containers         = make(map[string]*Container)
)

func monitorSignals() <-chan bool {
	signals := make(chan os.Signal, 1)
	done := make(chan bool, 1)

	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-signals
		fmt.Printf("Received %s signal\n", sig)
		done <- true
	}()

	return done
}

func createLogger() *zap.SugaredLogger {
	zapConfig := zap.NewDevelopmentConfig()
	zapConfig.DisableCaller = true
	zapConfig.DisableStacktrace = true
	zapConfig.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	zapConfig.OutputPaths = []string{"stdout"}
	zapConfig.ErrorOutputPaths = []string{"stdout"}
	logger, _ := zapConfig.Build()
	return logger.Sugar()
}

func createTemplateGenerator(templates []TemplateConfig) *TemplateGenerator {
	templateGenerator := NewTemplateGenerator()
	for _, template := range templates {
		templateGenerator.AddTemplate(template)
	}
	return templateGenerator
}

func createCertificateManager(config AcmeConfig) {
	certificateManager = NewCertificateManager(logger, config.Endpoint, config.KeyType, config.DnsProvider, config.CacheLocation)
	err := certificateManager.Init(config.Email)
	if err != nil {
		panic(err)
	}
}

func main() {
	logger = createLogger()
	logger.Info("Dotege is starting")

	doneChan := monitorSignals()
	config = createConfig()

	var err error
	dockerStopChan := make(chan struct{})
	dockerClient, err = client.NewEnvClient()
	if err != nil {
		panic(err)
	}

	templateGenerator := createTemplateGenerator(config.Templates)
	createCertificateManager(config.Acme)

	jitterTimer := time.NewTimer(time.Minute)
	redeployTimer := time.NewTicker(time.Hour * 24)
	updatedContainers := make(map[string]*Container)

	go func() {
		err := monitorContainers(dockerClient, dockerStopChan, func(container *Container) {
			containers[container.Name] = container
			updatedContainers[container.Name] = container
			jitterTimer.Reset(100 * time.Millisecond)
		}, func(name string) {
			delete(updatedContainers, name)
			delete(containers, name)
			jitterTimer.Reset(100 * time.Millisecond)
		})

		if err != nil {
			logger.Fatal("Error monitoring containers: ", err.Error())
		}
	}()

	go func() {
		for {
			select {
			case <-jitterTimer.C:
				hostnames := getHostnames(containers)
				updated := templateGenerator.Generate(Context{
					Containers: containers,
					Hostnames:  hostnames,
				})

				for name, container := range updatedContainers {
					certDeployed := deployCertForContainer(container)
					updated = updated || certDeployed
					delete(updatedContainers, name)
				}

				if updated {
					signalContainer()
				}
			case <-redeployTimer.C:
				logger.Info("Performing periodic certificate refresh")
				for _, container := range containers {
					deployCertForContainer(container)
					signalContainer()
				}
			}
		}
	}()

	<-doneChan

	dockerStopChan <- struct{}{}
	err = dockerClient.Close()
	if err != nil {
		panic(err)
	}
}

func signalContainer() {
	for _, s := range config.Signals {
		container, ok := containers[s.Name]
		if ok {
			logger.Debugf("Killing container %s with signal %s", s.Name, s.Signal)
			err := dockerClient.ContainerKill(context.Background(), container.Id, s.Signal)
			if err != nil {
				logger.Errorf("Unable to send signal %s to container %s: %s", s.Signal, s.Name, err.Error())
			}
		} else {
			logger.Warnf("Couldn't signal container %s as it is not running", s.Name)
		}
	}
}

func getHostnamesForContainer(container *Container) []string {
	if label, ok := container.Labels[config.Labels.Hostnames]; ok {
		return applyWildcards(splitList(label), config.WildCardDomains)
	} else {
		return []string{}
	}
}

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

func wildcardMatches(wildcard, domain string) bool {
	if len(domain) <= len(wildcard) {
		return false
	}

	pivot := len(domain) - len(wildcard) - 1
	start := domain[:pivot]
	end := domain[pivot+1:]
	return domain[pivot] == '.' && end == wildcard && !strings.ContainsRune(start, '.')
}

func getHostnames(containers map[string]*Container) (hostnames map[string]*Hostname) {
	hostnames = make(map[string]*Hostname)
	for _, container := range containers {
		if label, ok := container.Labels[config.Labels.Hostnames]; ok {
			names := splitList(label)
			if hostname, ok := hostnames[names[0]]; ok {
				hostname.Containers = append(hostname.Containers, container)
			} else {
				hostnames[names[0]] = &Hostname{
					Name:            names[0],
					Alternatives:    make(map[string]bool),
					Containers:      []*Container{container},
					CertDestination: config.DefaultCertDestination,
				}
			}
			addAlternatives(hostnames[names[0]], names[1:])

			if label, ok = container.Labels[config.Labels.RequireAuth]; ok {
				hostnames[names[0]].RequiresAuth = true
				hostnames[names[0]].AuthGroup = label
			}
		}
	}
	return
}

func addAlternatives(hostname *Hostname, alternatives []string) {
	for _, alternative := range alternatives {
		hostname.Alternatives[alternative] = true
	}
}

func deployCertForContainer(container *Container) bool {
	hostnames := getHostnamesForContainer(container)
	if len(hostnames) == 0 {
		logger.Debugf("No labels found for container %s", container.Name)
		return false
	}

	err, cert := certificateManager.GetCertificate(hostnames)
	if err != nil {
		logger.Warnf("Unable to generate certificate for %s: %s", container.Name, err.Error())
		return false
	} else {
		return deployCert(cert)
	}
}

func deployCert(certificate *SavedCertificate) bool {
	name := fmt.Sprintf("%s.pem", strings.ReplaceAll(certificate.Domains[0], "*", "_"))
	target := path.Join(config.DefaultCertDestination, name)
	content := append(certificate.Certificate, certificate.PrivateKey...)

	buf, _ := ioutil.ReadFile(target)
	if bytes.Equal(buf, content) {
		logger.Debugf("Certificate was up to date: %s", target)
		return false
	}

	err := ioutil.WriteFile(target, content, 0700)
	if err != nil {
		logger.Warnf("Unable to write certificate %s - %s", target, err.Error())
		return false
	} else {
		logger.Infof("Updated certificate file %s", target)
		return true
	}
}
