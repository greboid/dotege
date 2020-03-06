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

var (
	logger     *zap.SugaredLogger
	config     *Config
	containers = make(Containers)
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

func createCertificateManager(config AcmeConfig) *CertificateManager {
	cm := NewCertificateManager(logger, config.Endpoint, config.KeyType, config.DnsProvider, config.CacheLocation)
	err := cm.Init(config.Email)
	if err != nil {
		panic(err)
	}
	return cm
}

func main() {
	logger = createLogger()
	logger.Info("Dotege is starting")

	doneChan := monitorSignals()
	config = createConfig()

	var err error
	ctx, cancel := context.WithCancel(context.Background())
	dockerClient, err := client.NewEnvClient()
	if err != nil {
		panic(err)
	}

	templateGenerator := createTemplateGenerator(config.Templates)
	certificateManager := createCertificateManager(config.Acme)
	containerMonitor := ContainerMonitor{client: dockerClient}

	jitterTimer := time.NewTimer(time.Minute)
	redeployTimer := time.NewTicker(time.Hour * 24)
	updatedContainers := make(map[string]*Container)
	containerEvents := make(chan ContainerEvent)

	go func() {
		if err := containerMonitor.monitor(ctx, containerEvents); err != nil {
			logger.Fatal("Error monitoring containers: ", err.Error())
		}
	}()

	go func() {
		for {
			select {
			case event := <-containerEvents:
				switch event.Operation {
				case Added:
					containers[event.Container.Name] = &event.Container
					updatedContainers[event.Container.Name] = &event.Container
					jitterTimer.Reset(100 * time.Millisecond)
				case Removed:
					delete(updatedContainers, event.Container.Name)
					delete(containers, event.Container.Name)
					jitterTimer.Reset(100 * time.Millisecond)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		for {
			select {
			case <-jitterTimer.C:
				updated := templateGenerator.Generate(containers.TemplateContext())

				for name, container := range updatedContainers {
					certDeployed := deployCertForContainer(certificateManager, container)
					updated = updated || certDeployed
					delete(updatedContainers, name)
				}

				if updated {
					signalContainer(dockerClient)
				}
			case <-redeployTimer.C:
				logger.Info("Performing periodic certificate refresh")
				for _, container := range containers {
					deployCertForContainer(certificateManager, container)
					signalContainer(dockerClient)
				}
			}
		}
	}()

	<-doneChan

	cancel()
	err = dockerClient.Close()
	if err != nil {
		panic(err)
	}
}

func signalContainer(dockerClient *client.Client) {
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
	if label, ok := container.Labels[labelVhost]; ok {
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

func deployCertForContainer(cm *CertificateManager, container *Container) bool {
	hostnames := getHostnamesForContainer(container)
	if len(hostnames) == 0 {
		logger.Debugf("No labels found for container %s", container.Name)
		return false
	}

	err, cert := cm.GetCertificate(hostnames)
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
