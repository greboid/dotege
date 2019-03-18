package main

import (
	"fmt"
	"github.com/csmith/dotege/model"
	"github.com/docker/docker/client"
	"github.com/xenolf/lego/certcrypto"
	"github.com/xenolf/lego/lego"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
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

func createConfig() *model.Config {
	return &model.Config{
		Templates: []model.TemplateConfig{
			{
				Source:      "./templates/haproxy.cfg.tpl",
				Destination: "haproxy.cfg",
			},
		},
		Labels: model.LabelConfig{
			Hostnames:   "com.chameth.vhost",
			RequireAuth: "com.chameth.auth",
		},
		Acme: model.AcmeConfig{
			DnsProvider:   "httpreq",
			Email:         "dotege.test@chameth.com",
			Endpoint:      lego.LEDirectoryStaging,
			KeyType:       certcrypto.EC256,
			CacheLocation: "/config/certs.json",
		},
		DefaultCertActions:     model.COMBINE | model.FLATTEN,
		DefaultCertDestination: "/data/certs/",
	}
}

func createTemplateGenerator(logger *zap.SugaredLogger, templates []model.TemplateConfig) *TemplateGenerator {
	templateGenerator := NewTemplateGenerator(logger)
	for _, template := range templates {
		templateGenerator.AddTemplate(template)
	}
	return templateGenerator
}

func createCertificateManager(logger *zap.SugaredLogger, config model.AcmeConfig) *CertificateManager {
	certificateManager := NewCertificateManager(logger, config.Endpoint, config.KeyType, config.DnsProvider, config.CacheLocation)
	err := certificateManager.Init(config.Email)
	if err != nil {
		panic(err)
	}
	return certificateManager
}

func main() {
	logger := createLogger()
	logger.Info("Dotege is starting")

	doneChan := monitorSignals()
	config := createConfig()

	dockerStopChan := make(chan struct{})
	dockerClient, err := client.NewEnvClient()
	if err != nil {
		panic(err)
	}

	templateGenerator := createTemplateGenerator(logger, config.Templates)
	certificateManager := createCertificateManager(logger, config.Acme)

	jitterTimer := time.NewTimer(time.Minute)
	redeployTimer := time.NewTicker(time.Hour * 24)
	containers := make(map[string]model.Container)

	go func() {
		err := monitorContainers(dockerClient, dockerStopChan, func(container model.Container) {
			containers[container.Name] = container
			jitterTimer.Reset(100 * time.Millisecond)
			err, _ = certificateManager.GetCertificate(getHostnamesForContainer(container, *config))
		}, func(name string) {
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
				templateGenerator.Generate(Context{
					Containers: containers,
					Hostnames:  hostnames,
				})
			case <-redeployTimer.C:
				logger.Info("Performing periodic certificate refresh")
				for _, container := range containers {
					err, _ = certificateManager.GetCertificate(getHostnamesForContainer(container, *config))
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

func getHostnamesForContainer(container model.Container, config model.Config) []string {
	if label, ok := container.Labels[config.Labels.Hostnames]; ok {
		return strings.Split(strings.Replace(label, ",", " ", -1), " ")
	} else {
		return []string{}
	}
}

func getHostnames(containers map[string]model.Container, config model.Config) (hostnames map[string]*model.Hostname) {
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
					Containers:      []model.Container{container},
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
