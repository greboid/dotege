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

func createTemplateGenerator(templates []TemplateConfig) *TemplateGenerator {
	templateGenerator := NewTemplateGenerator()
	for _, template := range templates {
		templateGenerator.AddTemplate(template)
	}
	return templateGenerator
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
			loggers.main.Fatal("Error monitoring containers: ", err.Error())
		}
	}()

	go func() {
		for {
			select {
			case event := <-containerEvents:
				switch event.Operation {
				case Added:
					loggers.main.Debugf("Container added: %s", event.Container.Name)
					loggers.containers.Debugf("New container with name %s has id: %s", event.Container.Name, event.Container.Id)
					containers[event.Container.Id] = &event.Container
					updatedContainers[event.Container.Id] = &event.Container
					jitterTimer.Reset(100 * time.Millisecond)
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
				loggers.main.Info("Performing periodic certificate refresh")
				updated := false

				for _, container := range containers {
					updated = updated || deployCertForContainer(certificateManager, container)
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
			if c.Name == c.Name {
				container = c
			}
		}

		if container != nil {
			loggers.main.Debugf("Killing container %s with signal %s", s.Name, s.Signal)
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
	hostnames := container.CertNames()
	if len(hostnames) == 0 {
		loggers.main.Debugf("No labels found for container %s", container.Name)
		return false
	}

	err, cert := cm.GetCertificate(hostnames)
	if err != nil {
		loggers.main.Warnf("Unable to generate certificate for %s: %s", container.Name, err.Error())
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
		loggers.main.Debugf("Certificate was up to date: %s", target)
		return false
	}

	err := ioutil.WriteFile(target, content, 0700)
	if err != nil {
		loggers.main.Warnf("Unable to write certificate %s - %s", target, err.Error())
		return false
	} else {
		loggers.main.Infof("Updated certificate file %s", target)
		return true
	}
}
