package main

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/docker/docker/client"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	loggers = struct {
		main       *zap.SugaredLogger
		headers    *zap.SugaredLogger
		hostnames  *zap.SugaredLogger
		containers *zap.SugaredLogger
	}{
		main:       createLogger(),
		headers:    zap.NewNop().Sugar(),
		hostnames:  zap.NewNop().Sugar(),
		containers: zap.NewNop().Sugar(),
	}

	config     *Config
	containers = make(Containers)
	GitSHA     string
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

func createTemplates(configs []TemplateConfig) Templates {
	var templates Templates
	for _, t := range configs {
		templates = append(templates, CreateTemplate(t.Source, t.Destination))
	}
	return templates
}

func createCertificateManager(config AcmeConfig) *CertificateManager {
	cm := NewCertificateManager(loggers.main, config.Endpoint, config.KeyType, config.DnsProvider, config.CacheLocation)
	err := cm.Init(config.Email)
	if err != nil {
		panic(err)
	}
	return cm
}

func main() {
	loggers.main.Infof("Dotege %s is starting", GitSHA)

	doneChan := monitorSignals()
	config = createConfig()

	setUpDebugLoggers()

	var err error
	ctx, cancel := context.WithCancel(context.Background())
	dockerClient, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		panic(err)
	}

	templates := createTemplates(config.Templates)
	var certificateManager *CertificateManager

	if config.CertificateDeployment != CertificateDeploymentDisabled {
		certificateManager = createCertificateManager(config.Acme)
	}

	containerMonitor := ContainerMonitor{client: dockerClient}

	jitterTimer := time.NewTimer(time.Minute)
	redeployTimer := time.NewTicker(time.Hour * 24)
	updatedContainers := make(map[string]*Container)
	containerEvents := make(chan ContainerEvent)

	go func() {
		if err := containerMonitor.monitor(ctx, containerEvents); err != nil {
			loggers.main.Fatal("Error monitoring containers: ", err.Error())
		}
	}()

	go func() {
		for {
			select {
			case event := <-containerEvents:
				switch event.Operation {
				case Added:
					if event.Container.Labels[labelProxyTag] == config.ProxyTag {
						loggers.main.Debugf("Container added: %s", event.Container.Name)
						loggers.containers.Debugf("New container with name %s has id: %s", event.Container.Name, event.Container.Id)
						containers[event.Container.Id] = &event.Container
						updatedContainers[event.Container.Id] = &event.Container
						jitterTimer.Reset(100 * time.Millisecond)
					} else {
						loggers.main.Debugf("Container ignored due to proxy tag: %s (wanted: '%s', got: '%s')", event.Container.Name, config.ProxyTag, event.Container.Labels[labelProxyTag])
					}
				case Removed:
					loggers.main.Debugf("Container removed: %s", event.Container.Id)

					_, inUpdated := updatedContainers[event.Container.Id]
					_, inExisting := containers[event.Container.Id]
					loggers.containers.Debugf(
						"Removed container with ID %s, was in updated containers: %t, main containers: %t",
						event.Container.Id,
						inUpdated,
						inExisting,
					)

					delete(updatedContainers, event.Container.Id)
					delete(containers, event.Container.Id)
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
				loggers.containers.Debugf("Processing updated containers: %v", updatedContainers)
				updated := templates.Generate(struct {
					Containers map[string]*Container
					Hostnames  map[string]*Hostname
					Groups     []string
					Users      []User
				}{
					containers,
					containers.Hostnames(),
					groups(config.Users),
					config.Users,
				})

				for name, container := range updatedContainers {
					certDeployed := deployCertForContainer(certificateManager, container)
					updated = updated || certDeployed
					delete(updatedContainers, name)
				}

				if updated {
					signalContainer(dockerClient)
				}
			case <-redeployTimer.C:
				loggers.main.Info("Performing periodic certificate refresh")
				updated := false

				for _, container := range containers {
					if deployCertForContainer(certificateManager, container) {
						updated = true
					}
				}

				if updated {
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

func setUpDebugLoggers() {
	if config.DebugContainers {
		loggers.containers = loggers.main
	}

	if config.DebugHeaders {
		loggers.headers = loggers.main
	}

	if config.DebugHostnames {
		loggers.hostnames = loggers.main
	}
}

func signalContainer(dockerClient *client.Client) {
	for _, s := range config.Signals {
		var container *Container
		for _, c := range containers {
			if c.Name == s.Name {
				container = c
			}
		}

		if container != nil {
			loggers.main.Debugf("Killing container %s (%s) with signal %s", container.Name, container.Id, s.Signal)
			err := dockerClient.ContainerKill(context.Background(), container.Id, s.Signal)
			if err != nil {
				loggers.main.Errorf("Unable to send signal %s to container %s: %s", s.Signal, s.Name, err.Error())
			}
		} else {
			loggers.main.Warnf("Couldn't signal container %s as it is not running", s.Name)
		}
	}
}

func deployCertForContainer(cm *CertificateManager, container *Container) bool {
	if config.CertificateDeployment == CertificateDeploymentDisabled {
		return false
	}

	hostnames := container.CertNames(config.WildCardDomains)
	if len(hostnames) == 0 {
		loggers.main.Debugf("No labels found for container %s", container.Name)
		return false
	}

	cert, err := cm.GetCertificate(hostnames)
	if err != nil {
		loggers.main.Warnf("Unable to generate certificate for %s: %s", container.Name, err.Error())
		return false
	} else if config.CertificateDeployment == CertificateDeploymentSplit {
		return deploySplitCert(cert)
	} else {
		return deployCombinedCert(cert)
	}
}

func deploySplitCert(certificate *SavedCertificate) bool {
	name := fmt.Sprintf("%s.pem", strings.ReplaceAll(certificate.Domains[0], "*", "_"))
	target := path.Join(config.DefaultCertDestination, name)

	buf, _ := ioutil.ReadFile(target)
	if bytes.Equal(buf, certificate.Certificate) {
		loggers.main.Debugf("Certificate was up to date: %s", target)
		return false
	}

	err := ioutil.WriteFile(target, certificate.Certificate, config.CertMode)
	if err != nil {
		loggers.main.Warnf("Unable to write certificate %s - %s", target, err.Error())
		return false
	}

	if err = os.Chown(target, config.CertUid, config.CertGid); err != nil {
		loggers.main.Warnf("Unable to chown certificate %s - %s", target, err.Error())
		return false
	}

	name = fmt.Sprintf("%s.key", strings.ReplaceAll(certificate.Domains[0], "*", "_"))
	target = path.Join(config.DefaultCertDestination, name)

	buf, _ = ioutil.ReadFile(target)
	if bytes.Equal(buf, certificate.PrivateKey) {
		loggers.main.Debugf("Key was up to date: %s", target)
		return false
	}

	err = ioutil.WriteFile(target, certificate.PrivateKey, config.CertMode)
	if err != nil {
		loggers.main.Warnf("Unable to write key %s - %s", target, err.Error())
		return false
	}

	if err = os.Chown(target, config.CertUid, config.CertGid); err != nil {
		loggers.main.Warnf("Unable to chown key %s - %s", target, err.Error())
		return false
	}

	loggers.main.Infof("Updated certificate file %s", target)
	return true
}

func deployCombinedCert(certificate *SavedCertificate) bool {
	name := fmt.Sprintf("%s.pem", strings.ReplaceAll(certificate.Domains[0], "*", "_"))
	target := path.Join(config.DefaultCertDestination, name)
	content := append(certificate.Certificate, certificate.PrivateKey...)

	buf, _ := ioutil.ReadFile(target)
	if bytes.Equal(buf, content) {
		loggers.main.Debugf("Certificate was up to date: %s", target)
		return false
	}

	err := ioutil.WriteFile(target, content, config.CertMode)
	if err != nil {
		loggers.main.Warnf("Unable to write certificate %s - %s", target, err.Error())
		return false
	}

	if err := os.Chown(target, config.CertUid, config.CertGid); err != nil {
		loggers.main.Warnf("Unable to chown certificate %s - %s", target, err.Error())
		return false
	}

	loggers.main.Infof("Updated certificate file %s", target)
	return true
}

func groups(users []User) []string {
	groups := make(map[string]bool)
	for i := range users {
		for j := range users[i].Groups {
			groups[users[i].Groups[j]] = true
		}
	}

	var res []string
	for g := range groups {
		res = append(res, g)
	}
	return res
}
