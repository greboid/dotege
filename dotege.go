package main

import (
	"fmt"
	"github.com/csmith/dotege/certs"
	"github.com/csmith/dotege/docker"
	"github.com/csmith/dotege/model"
	"github.com/docker/docker/client"
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

func main() {
	zapConfig := zap.NewDevelopmentConfig()
	zapConfig.DisableCaller = true
	zapConfig.DisableStacktrace = true
	zapConfig.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	zapConfig.OutputPaths = []string{"stdout"}
	zapConfig.ErrorOutputPaths = []string{"stdout"}
	logger, _ := zapConfig.Build()
	sugar := logger.Sugar()
	sugar.Info("Dotege is starting")

	done := monitorSignals()
	containerChan := make(chan model.Container, 1)
	expiryChan := make(chan string, 1)
	config := model.Config{
		Labels: model.LabelConfig{
			Hostnames: "com.chameth.vhost",
		},
		DefaultCertActions:     model.COMBINE | model.FLATTEN,
		DefaultCertDestination: "/data/certs/",
	}

	cli, err := client.NewEnvClient()
	if err != nil {
		panic(err)
	}

	certMonitor := certs.NewCertificateManager(sugar)
	certMonitor.AddDirectory("/data/certrequests/certs")

	templateGenerator := NewTemplateGenerator(sugar)
	templateGenerator.AddTemplate(model.TemplateConfig{
		Source:      "./templates/domains.txt.tpl",
		Destination: "/data/certrequests/domains.txt",
	})
	templateGenerator.AddTemplate(model.TemplateConfig{
		Source:      "./templates/haproxy.cfg.tpl",
		Destination: "haproxy.cfg",
	})

	monitor := docker.NewContainerMonitor(sugar, cli, containerChan, expiryChan)
	go monitor.Monitor()

	go func() {
		containers := make(map[string]model.Container)
		timer := time.NewTimer(time.Hour)
		timer.Stop()

		for {
			select {
			case container := <-containerChan:
				containers[container.Name] = container
				timer.Reset(100 * time.Millisecond)
			case name := <-expiryChan:
				delete(containers, name)
				timer.Reset(100 * time.Millisecond)
			case <-timer.C:
				templateGenerator.Generate(Context{
					Containers: containers,
					Hostnames:  getHostnames(containers, config),
				})
			}
		}
	}()

	<-done

	err = cli.Close()
	if err != nil {
		panic(err)
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
		}
	}
	return
}

func addAlternatives(hostname *model.Hostname, alternatives []string) {
	for _, alternative := range alternatives {
		hostname.Alternatives[alternative] = true
	}
}
