package main

import (
	"bytes"
	"context"
	"fmt"
	"github.com/csmith/dotege/model"
	"github.com/docker/docker/client"
	"github.com/xenolf/lego/certcrypto"
	"github.com/xenolf/lego/lego"
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

const (
	envCertDestinationKey         = "DOTEGE_CERT_DESTINATION"
	envCertDestinationDefault     = "/data/certs/"
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
)

var (
	logger             *zap.SugaredLogger
	certificateManager *CertificateManager
	config             *model.Config
	dockerClient       *client.Client
	containers         = make(map[string]*model.Container)
)

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

func createSignalConfig() []model.ContainerSignal {
	name := optionalVar(envSignalContainerKey, envSignalContainerDefault)
	if name == envSignalContainerDefault {
		return []model.ContainerSignal{}
	} else {
		return []model.ContainerSignal{
			{
				Name:   name,
				Signal: optionalVar(envSignalTypeKey, envSignalTypeDefault),
			},
		}
	}
}

func createConfig() {
	config = &model.Config{
		Templates: []model.TemplateConfig{
			{
				Source:      optionalVar(envTemplateSourceKey, envTemplateSourceDefault),
				Destination: optionalVar(envTemplateDestinationKey, envTemplateDestinationDefault),
			},
		},
		Labels: model.LabelConfig{
			Hostnames:   "com.chameth.vhost",
			RequireAuth: "com.chameth.auth",
		},
		Acme: model.AcmeConfig{
			DnsProvider:   requiredVar(envDnsProviderKey),
			Email:         requiredVar(envAcmeEmailKey),
			Endpoint:      optionalVar(envAcmeEndpointKey, lego.LEDirectoryProduction),
			KeyType:       certcrypto.KeyType(optionalVar(envAcmeKeyTypeKey, envAcmeKeyTypeDefault)),
			CacheLocation: optionalVar(envAcmeCacheLocationKey, envAcmeCacheLocationDefault),
		},
		Signals:                createSignalConfig(),
		DefaultCertActions:     model.COMBINE | model.FLATTEN,
		DefaultCertDestination: optionalVar(envCertDestinationKey, envCertDestinationDefault),
	}
}

func createTemplateGenerator(templates []model.TemplateConfig) *TemplateGenerator {
	templateGenerator := NewTemplateGenerator(logger)
	for _, template := range templates {
		templateGenerator.AddTemplate(template)
	}
	return templateGenerator
}

func createCertificateManager(config model.AcmeConfig) {
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
	createConfig()

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
	updatedContainers := make(map[string]*model.Container)

	go func() {
		err := monitorContainers(dockerClient, dockerStopChan, func(container *model.Container) {
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
				hostnames := getHostnames(containers, *config)
				updated := templateGenerator.Generate(Context{
					Containers: containers,
					Hostnames:  hostnames,
				})

				for name, container := range updatedContainers {
					certDeployed := deployCertForContainer(container)
					updated = updated || certDeployed
					delete(updatedContainers, name)
				}

				signalContainer()
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
			err := dockerClient.ContainerKill(context.Background(), container.Id, s.Signal)
			if err != nil {
				logger.Errorf("Unable to send signal %s to container %s: %s", s.Signal, s.Name, err.Error())
			}
		} else {
			logger.Warnf("Couldn't signal container %s as it is not running", s.Name)
		}
	}
}

func getHostnamesForContainer(container *model.Container) []string {
	if label, ok := container.Labels[config.Labels.Hostnames]; ok {
		return strings.Split(strings.Replace(label, ",", " ", -1), " ")
	} else {
		return []string{}
	}
}

func getHostnames(containers map[string]*model.Container, config model.Config) (hostnames map[string]*model.Hostname) {
	hostnames = make(map[string]*model.Hostname)
	for _, container := range containers {
		if label, ok := container.Labels[config.Labels.Hostnames]; ok {
			names := strings.Split(strings.Replace(label, ",", " ", -1), " ")
			if hostname, ok := hostnames[names[0]]; ok {
				hostname.Containers = append(hostname.Containers, container)
			} else {
				hostnames[names[0]] = &model.Hostname{
					Name:            names[0],
					Alternatives:    make(map[string]bool),
					Containers:      []*model.Container{container},
					CertActions:     config.DefaultCertActions,
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

func addAlternatives(hostname *model.Hostname, alternatives []string) {
	for _, alternative := range alternatives {
		hostname.Alternatives[alternative] = true
	}
}

func deployCertForContainer(container *model.Container) bool {
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
	target := path.Join(config.DefaultCertDestination, fmt.Sprintf("%s.pem", certificate.Domains[0]))
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
